package pipeline

import (
	"context"
	"fmt"
	"io"

	"github.com/Bin-hy/bin-rag/internal/chunker"
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
type Pipeline interface {
	Ingest(ctx context.Context, req IngestRequest) ([]string, error)
}

type defaultPipeline struct {
	loader      loader.Loader
	chunker     chunker.Chunker
	embedder    embedding.Embedder
	vectorstore vectorstore.VectorStore
	chunkConfig chunker.ChunkerConfig
	bm25Index   retriever.BM25Index
}

// NewPipeline 创建文档入库 Pipeline
func NewPipeline(
	ld loader.Loader,
	ch chunker.Chunker,
	emb embedding.Embedder,
	vs vectorstore.VectorStore,
	cfg chunker.ChunkerConfig,
	bm25 retriever.BM25Index,
) Pipeline {
	return &defaultPipeline{
		loader:      ld,
		chunker:     ch,
		embedder:    emb,
		vectorstore: vs,
		chunkConfig: cfg,
		bm25Index:   bm25,
	}
}

// Ingest 执行 Load → Chunk → Embed → Store，返回生成的 chunk IDs
func (p *defaultPipeline) Ingest(ctx context.Context, req IngestRequest) ([]string, error) {
	// Load
	result, err := p.loader.Load(ctx, req.Reader, req.Info)
	if err != nil {
		return nil, fmt.Errorf("加载文档失败: %w", err)
	}

	if result.Document == nil || len(result.Document.Blocks) == 0 {
		return nil, nil
	}

	// Chunk
	chunks := p.chunker.Chunk(result.Document, p.chunkConfig)
	if len(chunks) == 0 {
		return nil, nil
	}

	// Embed
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vectors, err := p.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("生成 Embedding 失败: %w", err)
	}

	if len(vectors) != len(chunks) {
		return nil, fmt.Errorf("向量数量(%d)与 Chunk 数量(%d)不匹配", len(vectors), len(chunks))
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
			},
		}
	}

	if err := p.vectorstore.Upsert(ctx, records); err != nil {
		return nil, fmt.Errorf("向量入库失败: %w", err)
	}

	// 更新 BM25 索引（携带知识库维度）
	if p.bm25Index != nil {
		for _, rec := range records {
			content, _ := rec.Payload["content"].(string)
			kbID, _ := rec.Payload["kb_id"].(string)
			p.bm25Index.Add(rec.ID, content, kbID)
		}
	}

	return chunkIDs, nil
}
