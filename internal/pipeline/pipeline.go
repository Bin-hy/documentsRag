package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/Bin-hy/bin-rag/internal/chunker"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/embedding"
	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
	"github.com/google/uuid"
)

// IngestRequest 入库请求（携带知识库与文档维度）
type IngestRequest struct {
	KBID       string
	DocumentID string
	Reader     io.Reader
	Info       loader.FileInfo
}

// Pipeline 文档入库编排接口
// Ingest 返回 (chunkIDs, warnings, error)；warnings 为非阻断告警（如视频音轨降级，spec N4）
type Pipeline interface {
	Ingest(ctx context.Context, req IngestRequest) ([]string, []string, error)
}

type defaultPipeline struct {
	loader      loader.Loader
	chunker     chunker.Chunker
	embedder    embedding.Embedder
	vectorstore vectorstore.VectorStore
	chunkConfig chunker.ChunkerConfig
	loaderCfg   config.LoaderConfig
	bm25Index   retriever.BM25Index
}

// NewPipeline 创建文档入库 Pipeline
func NewPipeline(
	ld loader.Loader,
	ch chunker.Chunker,
	emb embedding.Embedder,
	vs vectorstore.VectorStore,
	cfg chunker.ChunkerConfig,
	lc config.LoaderConfig,
	bm25 retriever.BM25Index,
) Pipeline {
	return &defaultPipeline{
		loader:      ld,
		chunker:     ch,
		embedder:    emb,
		vectorstore: vs,
		chunkConfig: cfg,
		loaderCfg:   lc,
		bm25Index:   bm25,
	}
}

// Ingest 执行 Load → Chunk → Embed → Store，返回生成的 chunk IDs 与非阻断告警
func (p *defaultPipeline) Ingest(ctx context.Context, req IngestRequest) ([]string, []string, error) {
	// 重试补偿：清理同一 document_id 的旧 chunk（向量 + BM25），防止重试产生孤儿向量
	if req.DocumentID != "" {
		if err := p.vectorstore.DeleteByFilter(ctx, map[string]any{"document_id": req.DocumentID}); err != nil {
			slog.Warn("清理旧 chunk 向量失败（继续入库）", "doc", req.DocumentID, "err", err)
		}
		if p.bm25Index != nil {
			p.bm25Index.RemoveByDoc(req.DocumentID)
		}
	}

	// Load
	result, err := p.loader.Load(ctx, req.Reader, req.Info)
	if err != nil {
		return nil, nil, fmt.Errorf("加载文档失败: %w", err)
	}

	// 可读文本校验（扫描件/空内容拒绝入库，防止污染知识库）
	if result.Document == nil || len(result.Document.Blocks) == 0 {
		return nil, nil, &loader.ErrNoReadableContent{
			Format:   req.Info.Filename,
			Readable: 0,
			MinChars: p.loaderCfg.MinReadableChars,
		}
	}
	if err := loader.ValidateReadable(result.Document, p.loaderCfg.MinReadableChars); err != nil {
		return nil, nil, err
	}

	// Chunk
	chunks := p.chunker.Chunk(result.Document, p.chunkConfig)
	if len(chunks) == 0 {
		return nil, result.Warnings, nil
	}

	// Embed
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vectors, err := p.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, nil, fmt.Errorf("生成 Embedding 失败: %w", err)
	}

	if len(vectors) != len(chunks) {
		return nil, nil, fmt.Errorf("向量数量(%d)与 Chunk 数量(%d)不匹配", len(vectors), len(chunks))
	}

	// Store（payload 携带知识库/文档/chunk 维度）
	records := make([]vectorstore.VectorRecord, len(chunks))
	chunkIDs := make([]string, len(chunks))
	for i, c := range chunks {
		chunkID := uuid.New().String()
		chunkIDs[i] = chunkID
		records[i] = vectorstore.VectorRecord{
			ID:     chunkID,
			Vector: vectors[i],
			Payload: map[string]any{
				"kb_id":           req.KBID,
				"document_id":     req.DocumentID,
				"chunk_id":        chunkID,
				"filename":        c.Metadata.DocFilename,
				"heading_context": c.Metadata.HeadingContext,
				"chunk_index":     c.Index,
				"content":         c.Content,
				"source_type":     c.Metadata.SourceType,
				"start_ms":        c.Metadata.StartMs,
				"end_ms":          c.Metadata.EndMs,
				"page_number":     c.Metadata.PageNumber,
				"heading":         c.Metadata.Heading,
				"anchor":          c.Metadata.Anchor,
			},
		}
	}

	if err := p.vectorstore.Upsert(ctx, records); err != nil {
		return nil, nil, fmt.Errorf("向量入库失败: %w", err)
	}

	// 更新 BM25 索引（携带知识库 + 文档维度，RemoveByDoc 补偿用）
	if p.bm25Index != nil {
		for _, rec := range records {
			content, _ := rec.Payload["content"].(string)
			kbID, _ := rec.Payload["kb_id"].(string)
			p.bm25Index.AddWithDocID(rec.ID, content, kbID, req.DocumentID)
		}
	}

	return chunkIDs, result.Warnings, nil
}
