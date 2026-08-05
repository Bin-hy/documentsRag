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

// Pipeline 文档入库编排接口
type Pipeline interface {
	Ingest(ctx context.Context, reader io.Reader, info loader.FileInfo) error
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

func (p *defaultPipeline) Ingest(ctx context.Context, reader io.Reader, info loader.FileInfo) error {
	// Load
	result, err := p.loader.Load(ctx, reader, info)
	if err != nil {
		return fmt.Errorf("加载文档失败: %w", err)
	}

	if result.Document == nil || len(result.Document.Blocks) == 0 {
		return nil
	}

	// Chunk
	chunks := p.chunker.Chunk(result.Document, p.chunkConfig)
	if len(chunks) == 0 {
		return nil
	}

	// Embed
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vectors, err := p.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("生成 Embedding 失败: %w", err)
	}

	if len(vectors) != len(chunks) {
		return fmt.Errorf("向量数量(%d)与 Chunk 数量(%d)不匹配", len(vectors), len(chunks))
	}

	// Store
	records := make([]vectorstore.VectorRecord, len(chunks))
	for i, c := range chunks {
		records[i] = vectorstore.VectorRecord{
			ID:     uuid.New().String(),
			Vector: vectors[i],
			Payload: map[string]any{
				"filename":        c.Metadata.DocFilename,
				"heading_context": c.Metadata.HeadingContext,
				"chunk_index":     c.Index,
				"content":         c.Content,
			},
		}
	}

	if err := p.vectorstore.Upsert(ctx, records); err != nil {
		return fmt.Errorf("向量入库失败: %w", err)
	}

	// 更新 BM25 索引
	if p.bm25Index != nil {
		for _, rec := range records {
			content, _ := rec.Payload["content"].(string)
			p.bm25Index.Add(rec.ID, content)
		}
	}

	return nil
}
