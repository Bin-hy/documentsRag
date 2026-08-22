package pipeline

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/chunker"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
)

type mockEmbedder struct {
	dim       int
	shouldErr bool
}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.shouldErr {
		return nil, errors.New("embed error")
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = make([]float32, m.dim)
	}
	return vectors, nil
}

type mockVectorStore struct {
	records   []vectorstore.VectorRecord
	shouldErr bool
}

func (m *mockVectorStore) Upsert(ctx context.Context, records []vectorstore.VectorRecord) error {
	if m.shouldErr {
		return errors.New("upsert error")
	}
	m.records = append(m.records, records...)
	return nil
}

func (m *mockVectorStore) Search(ctx context.Context, req vectorstore.SearchRequest) ([]vectorstore.SearchResult, error) {
	return nil, nil
}

func (m *mockVectorStore) Delete(ctx context.Context, ids []string) error {
	return nil
}

func (m *mockVectorStore) DeleteByFilter(ctx context.Context, filter map[string]any) error {
	return nil
}

func (m *mockVectorStore) Get(ctx context.Context, id string) (map[string]any, bool, error) {
	return nil, false, nil
}

func (m *mockVectorStore) EnsureCollection(ctx context.Context) error {
	return nil
}

func (m *mockVectorStore) Close() error {
	return nil
}

func TestIngestSuccess(t *testing.T) {
	emb := &mockEmbedder{dim: 4}
	vs := &mockVectorStore{}

	p := NewPipeline(
		loader.NewLoader(),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{
			Strategy:  chunker.StrategyRecursive,
			ChunkSize: 500,
		},
		config.LoaderConfig{MinReadableChars: 20},
		nil,
	)

	input := "# 标题\n\n这是正文段落，描述系统的功能特性与设计目标。\n\n## 二级标题\n\n这是第二段内容，包含更多说明文字用于测试。"
	reader := strings.NewReader(input)
	info := loader.FileInfo{Filename: "test.md", Size: int64(len(input))}

	_, _, err := p.Ingest(context.Background(), IngestRequest{
		KBID:       "kb-1",
		DocumentID: "doc-1",
		Reader:     reader,
		Info:       info,
	})
	if err != nil {
		t.Fatalf("Ingest 失败: %v", err)
	}

	if len(vs.records) == 0 {
		t.Fatal("期望至少有 1 条 record 入库")
	}

	for _, r := range vs.records {
		if r.Payload["filename"] != "test.md" {
			t.Errorf("filename 期望 test.md，实际 %v", r.Payload["filename"])
		}
		if r.Payload["kb_id"] != "kb-1" {
			t.Errorf("kb_id 期望 kb-1，实际 %v", r.Payload["kb_id"])
		}
		if r.Payload["document_id"] != "doc-1" {
			t.Errorf("document_id 期望 doc-1，实际 %v", r.Payload["document_id"])
		}
		if _, ok := r.Payload["chunk_id"]; !ok {
			t.Error("record 缺少 chunk_id 字段")
		}
		if _, ok := r.Payload["heading_context"]; !ok {
			t.Error("record 缺少 heading_context 字段")
		}
		if _, ok := r.Payload["chunk_index"]; !ok {
			t.Error("record 缺少 chunk_index 字段")
		}
		if _, ok := r.Payload["content"]; !ok {
			t.Error("record 缺少 content 字段")
		}
		if len(r.Vector) != 4 {
			t.Errorf("向量维度期望 4，实际 %d", len(r.Vector))
		}
		if r.ID == "" {
			t.Error("record ID 不应为空")
		}
	}
}

func TestIngestEmbedError(t *testing.T) {
	emb := &mockEmbedder{dim: 4, shouldErr: true}
	vs := &mockVectorStore{}

	p := NewPipeline(
		loader.NewLoader(),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{Strategy: chunker.StrategyRecursive, ChunkSize: 500},
		config.LoaderConfig{MinReadableChars: 20},
		nil,
	)

	input := "一些文本内容"
	_, _, err := p.Ingest(context.Background(), IngestRequest{
		KBID:       "kb-1",
		DocumentID: "doc-1",
		Reader:     strings.NewReader(input),
		Info:       loader.FileInfo{Filename: "test.txt"},
	})
	if err == nil {
		t.Fatal("Embed 失败时 Ingest 应返回 error")
	}
}

func TestIngestUpsertError(t *testing.T) {
	emb := &mockEmbedder{dim: 4}
	vs := &mockVectorStore{shouldErr: true}

	p := NewPipeline(
		loader.NewLoader(),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{Strategy: chunker.StrategyRecursive, ChunkSize: 500},
		config.LoaderConfig{MinReadableChars: 20},
		nil,
	)

	input := "一些文本内容"
	_, _, err := p.Ingest(context.Background(), IngestRequest{
		KBID:       "kb-1",
		DocumentID: "doc-1",
		Reader:     strings.NewReader(input),
		Info:       loader.FileInfo{Filename: "test.txt"},
	})
	if err == nil {
		t.Fatal("Upsert 失败时 Ingest 应返回 error")
	}
}

// 确保 mockVectorStore 实现了 io.Closer（如果有）
var _ io.Closer = (*mockVectorStore)(nil)

// 扫描件/空内容：无可读文本时 Ingest 返回错误，不调用 embedder 与 vectorstore
func TestIngestNoReadableContent(t *testing.T) {
	emb := &mockEmbedder{dim: 4}
	vs := &mockVectorStore{}

	p := NewPipeline(
		loader.NewLoader(),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{Strategy: chunker.StrategyRecursive, ChunkSize: 500},
		config.LoaderConfig{MinReadableChars: 20},
		nil,
	)

	// 纯乱码/图像指令文本（无可读文本）
	input := "q 595.44 0 0 841.68 0.00 0.00 cm 1 g /Im10 Do Q"
	_, _, err := p.Ingest(context.Background(), IngestRequest{
		KBID:       "kb-1",
		DocumentID: "doc-1",
		Reader:     strings.NewReader(input),
		Info:       loader.FileInfo{Filename: "scan.pdf"},
	})
	if err == nil {
		t.Fatal("无可读文本应返回 error")
	}
	var nerr *loader.ErrNoReadableContent
	if !errors.As(err, &nerr) {
		t.Fatalf("错误类型应为 ErrNoReadableContent，实际 %T", err)
	}
	if len(vs.records) != 0 {
		t.Errorf("拒绝后不应写入向量库，实际 %d 条", len(vs.records))
	}
}

// fakeLoaderWithWarnings 返回带非阻断告警的加载结果（验证 warnings 透传，spec N4）
type fakeLoaderWithWarnings struct{}

func (fakeLoaderWithWarnings) Load(ctx context.Context, reader io.Reader, info loader.FileInfo, opts ...loader.LoadOptions) (*loader.LoadResult, error) {
	return &loader.LoadResult{
		Document: &loader.Document{
			Blocks:   []loader.Block{{Type: loader.BlockParagraph, Content: "可检索的正文内容，包含足够多的文字描述用于向量化。"}},
			Metadata: loader.DocumentMeta{Filename: info.Filename},
		},
		Warnings: []string{"multimedia.speech 未配置，跳过视频音轨转写"},
	}, nil
}

// warnings 透传：Ingest 返回的 warnings 与 LoadResult.Warnings 一致
func TestIngestWarningsPassthrough(t *testing.T) {
	emb := &mockEmbedder{dim: 4}
	vs := &mockVectorStore{}
	p := NewPipeline(
		fakeLoaderWithWarnings{},
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{Strategy: chunker.StrategyRecursive, ChunkSize: 50, ChunkOverlap: 10},
		config.LoaderConfig{MinReadableChars: 5},
		nil,
	)
	_, warnings, err := p.Ingest(context.Background(), IngestRequest{
		KBID:       "kb-1",
		DocumentID: "doc-1",
		Reader:     strings.NewReader("ignored"),
		Info:       loader.FileInfo{Filename: "a.mp4"},
	})
	if err != nil {
		t.Fatalf("Ingest 失败: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "跳过视频音轨转写") {
		t.Errorf("warnings 透传错误: %v", warnings)
	}
}

// —— 多媒体端到端（单元级）：真实 loader + mock providers 走完整入库链路 ——

type pipeMockVision struct{ text string }

func (m *pipeMockVision) Describe(_ context.Context, _ []byte, _ loader.VisionOptions) (string, error) {
	return m.text, nil
}

type pipeMockExtractor struct{ frames []loader.VideoFrame }

func (m *pipeMockExtractor) SampleFrames(_ context.Context, _ string, _ loader.FrameStrategyConfig) ([]loader.VideoFrame, error) {
	return m.frames, nil
}

// pipeMockProber / pipeMockAudioExtractor：视频 parser 依赖（测试桩）
type pipeMockProber struct{}

func (pipeMockProber) Probe(_ context.Context, _ string) (loader.MediaInfo, error) {
	return loader.MediaInfo{DurationMs: 10000, HasAudio: true}, nil
}

type pipeMockAudioExtractor struct{}

func (pipeMockAudioExtractor) Extract(_ context.Context, _ string) (string, error) {
	return "", nil
}

// 场景 1/3（单元级）：图片与视频经真实 loader → 全链路入库，文本化内容进 chunk payload
func TestIngestMultimediaEndToEnd(t *testing.T) {
	reg := loader.NewRegistry()
	reg.Register(loader.NewImageParser(&pipeMockVision{text: "一张架构图，包含数据库与 API 层"}))
	reg.Register(loader.NewVideoParser(&pipeMockVision{text: "画面：登录界面"}, nil,
		&pipeMockExtractor{frames: []loader.VideoFrame{{TimeMs: 0, Data: []byte("f0")}}},
		pipeMockProber{}, pipeMockAudioExtractor{}, loader.FrameStrategyConfig{IntervalSec: 10}))

	emb := &mockEmbedder{dim: 4}
	vs := &mockVectorStore{}
	p := NewPipeline(
		loader.NewLoaderWithRegistry(reg),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{Strategy: chunker.StrategyRecursive, ChunkSize: 50, ChunkOverlap: 10},
		config.LoaderConfig{MinReadableChars: 5},
		nil,
	)

	// 图片
	imgIDs, _, err := p.Ingest(context.Background(), IngestRequest{
		KBID: "kb-1", DocumentID: "doc-img", Reader: strings.NewReader("fake-png"), Info: loader.FileInfo{Filename: "架构图.png"},
	})
	if err != nil {
		t.Fatalf("图片入库失败: %v", err)
	}
	if len(imgIDs) == 0 {
		t.Fatal("图片应产出 chunk")
	}

	// 视频（无 speech：音轨降级 warning 透传）
	vidIDs, warnings, err := p.Ingest(context.Background(), IngestRequest{
		KBID: "kb-1", DocumentID: "doc-vid", Reader: strings.NewReader("fake-video"), Info: loader.FileInfo{Filename: "演示.mp4"},
	})
	if err != nil {
		t.Fatalf("视频入库失败: %v", err)
	}
	if len(vidIDs) == 0 {
		t.Fatal("视频应产出 chunk")
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "跳过视频音轨转写") {
		t.Errorf("视频应带音轨降级 warning，实际 %v", warnings)
	}

	// payload 内容 = 视觉描述文本（检索数据源），filename 为原文件名（spec F8）
	var content string
	var filename string
	for _, r := range vs.records {
		if r.Payload["document_id"] == "doc-img" {
			content = r.Payload["content"].(string)
			filename = r.Payload["filename"].(string)
			break
		}
	}
	if !strings.Contains(content, "架构图") {
		t.Errorf("图片 chunk 内容应含视觉描述，实际 %q", content)
	}
	if filename != "架构图.png" {
		t.Errorf("引用来源应为原文件名，实际 %q", filename)
	}
	// 时间戳贯通：payload 含 source_type/start_ms/end_ms（spec F7）
	var srcType string
	for _, r := range vs.records {
		if r.Payload["document_id"] == "doc-img" {
			srcType = r.Payload["source_type"].(string)
			if r.Payload["start_ms"] != int64(0) {
				t.Errorf("图片 start_ms 应为 0，实际 %v", r.Payload["start_ms"])
			}
			break
		}
	}
	if srcType != "image" {
		t.Errorf("图片来源类型应为 image，实际 %q", srcType)
	}
}
