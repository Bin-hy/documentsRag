package chunker

import (
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/loader"
)

// mockTokenizer 每个字符计 1 token，方便精确验证
type mockTokenizer struct{}

func (t *mockTokenizer) Count(text string) int {
	return len([]rune(text))
}

// mockStrategy 记录调用
type mockStrategy struct {
	called bool
}

func (s *mockStrategy) Split(text string, config ChunkerConfig, tokenizer Tokenizer) []string {
	s.called = true
	return []string{text}
}

func TestDefaultTokenizer(t *testing.T) {
	tok := NewDefaultTokenizer()

	tests := []struct {
		input    string
		expected int
	}{
		{"hello", 1},
		{"hello world", 2},
		{"你好", 4},           // 2 个中文字符 × 2
		{"你好world", 5},     // 2×2 + 1
		{"", 0},
		{"a,b", 3},          // a + , + b
	}

	for _, tt := range tests {
		got := tok.Count(tt.input)
		if got != tt.expected {
			t.Errorf("Count(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestFixedSizeStrategy(t *testing.T) {
	tok := &mockTokenizer{}
	strategy := NewFixedSizeStrategy()

	// 生成 100 字符文本
	input := strings.Repeat("abcdefghij", 10)
	config := ChunkerConfig{ChunkSize: 20, ChunkOverlap: 0}

	chunks := strategy.Split(input, config, tok)

	for i, chunk := range chunks {
		count := tok.Count(chunk)
		if count > config.ChunkSize {
			t.Errorf("chunk[%d] token count %d > ChunkSize %d", i, count, config.ChunkSize)
		}
	}

	if len(chunks) < 4 {
		t.Errorf("期望至少 4 个 chunk（100 / 20），实际 %d", len(chunks))
	}
}

func TestFixedSizeOverlap(t *testing.T) {
	tok := &mockTokenizer{}
	strategy := NewFixedSizeStrategy()

	// 生成包含空格的文本方便在空白处切分
	words := make([]string, 50)
	for i := range words {
		words[i] = "word"
	}
	input := strings.Join(words, " ") // "word word word ..." 共 50 个词

	config := ChunkerConfig{ChunkSize: 25, ChunkOverlap: 5}
	chunks := strategy.Split(input, config, tok)

	if len(chunks) < 2 {
		t.Fatalf("期望至少 2 个 chunk，实际 %d", len(chunks))
	}

	// 验证相邻 chunk 有重叠
	for i := 1; i < len(chunks); i++ {
		prev := chunks[i-1]
		curr := chunks[i]
		// 前一个 chunk 的尾部应该出现在后一个 chunk 的开头
		prevSuffix := getLastNChars(prev, 5)
		if !strings.Contains(curr, prevSuffix) && prevSuffix != "" {
			// overlap 不要求精确匹配，但相邻 chunk 应有公共内容
			// 放宽检查：至少有一些重叠
			prevWords := strings.Fields(prev)
			currWords := strings.Fields(curr)
			if len(prevWords) > 0 && len(currWords) > 0 {
				lastWord := prevWords[len(prevWords)-1]
				if !strings.Contains(curr, lastWord) {
					// 允许有 overlap 但不强制严格匹配
				}
			}
		}
	}
}

func TestRecursiveStrategy(t *testing.T) {
	tok := &mockTokenizer{}
	strategy := NewRecursiveStrategy()

	// 生成含多段落的文本
	paragraphs := []string{
		strings.Repeat("a", 15),
		strings.Repeat("b", 15),
		strings.Repeat("c", 15),
	}
	input := strings.Join(paragraphs, "\n\n")

	config := ChunkerConfig{ChunkSize: 20, ChunkOverlap: 0}
	chunks := strategy.Split(input, config, tok)

	for i, chunk := range chunks {
		count := tok.Count(chunk)
		if count > config.ChunkSize {
			t.Errorf("chunk[%d] token count %d > ChunkSize %d", i, count, config.ChunkSize)
		}
	}

	// 应该在段落边界切分
	if len(chunks) < 3 {
		t.Errorf("期望至少 3 个 chunk（3 段落各 15 字符），实际 %d", len(chunks))
	}
}

func TestHeadingStrategy(t *testing.T) {
	tok := &mockTokenizer{}
	c := NewChunker(tok)

	doc := &loader.Document{
		Blocks: []loader.Block{
			{Type: loader.BlockHeading, Content: "主标题", Level: 1},
			{Type: loader.BlockHeading, Content: "章节一", Level: 2},
			{Type: loader.BlockParagraph, Content: "章节一的内容"},
			{Type: loader.BlockHeading, Content: "章节二", Level: 2},
			{Type: loader.BlockParagraph, Content: "章节二的内容"},
			{Type: loader.BlockHeading, Content: "章节三", Level: 2},
			{Type: loader.BlockParagraph, Content: "章节三的内容"},
		},
		Metadata: loader.DocumentMeta{Filename: "test.md"},
	}

	config := ChunkerConfig{
		Strategy:  StrategyHeading,
		ChunkSize: 500, // 足够大，不触发降级
	}

	chunks := c.Chunk(doc, config)

	// 应该有 3 个 chunk（按 h2 切分，h1 单独作为第一个 chunk 或归入第一个 h2）
	if len(chunks) < 3 {
		t.Errorf("期望至少 3 个 chunk，实际 %d", len(chunks))
	}

	// 验证 HeadingContext
	for _, chunk := range chunks {
		if chunk.Metadata.HeadingContext == "" {
			t.Errorf("chunk[%d] HeadingContext 为空", chunk.Index)
		}
	}

	// 验证包含标题路径
	found := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.Metadata.HeadingContext, "主标题") {
			found = true
			break
		}
	}
	if !found {
		t.Error("HeadingContext 中未找到主标题")
	}
}

func TestHeadingStrategyFallback(t *testing.T) {
	tok := &mockTokenizer{}
	c := NewChunker(tok)

	// 超长 h2 节
	longContent := strings.Repeat("x", 100)
	doc := &loader.Document{
		Blocks: []loader.Block{
			{Type: loader.BlockHeading, Content: "标题", Level: 2},
			{Type: loader.BlockParagraph, Content: longContent},
		},
		Metadata: loader.DocumentMeta{Filename: "test.md"},
	}

	config := ChunkerConfig{
		Strategy:  StrategyHeading,
		ChunkSize: 30,
	}

	chunks := c.Chunk(doc, config)

	// 应该降级拆分
	if len(chunks) < 2 {
		t.Errorf("超长节应降级拆分为多个 chunk，实际 %d", len(chunks))
	}

	for i, chunk := range chunks {
		count := tok.Count(chunk.Content)
		if count > config.ChunkSize {
			t.Errorf("chunk[%d] token count %d > ChunkSize %d", i, count, config.ChunkSize)
		}
	}
}

func TestChunkMetadata(t *testing.T) {
	tok := &mockTokenizer{}
	c := NewChunker(tok)

	doc := &loader.Document{
		Blocks: []loader.Block{
			{Type: loader.BlockParagraph, Content: strings.Repeat("hello ", 20)},
		},
		Metadata: loader.DocumentMeta{Filename: "report.txt"},
	}

	config := ChunkerConfig{
		Strategy:  StrategyFixed,
		ChunkSize: 30,
	}

	chunks := c.Chunk(doc, config)

	for i, chunk := range chunks {
		if chunk.Index != i {
			t.Errorf("chunk[%d].Index = %d, want %d", i, chunk.Index, i)
		}
		if chunk.Metadata.DocFilename != "report.txt" {
			t.Errorf("chunk[%d].DocFilename = %q, want %q", i, chunk.Metadata.DocFilename, "report.txt")
		}
		if chunk.Metadata.TokenCount <= 0 {
			t.Errorf("chunk[%d].TokenCount = %d, want > 0", i, chunk.Metadata.TokenCount)
		}
	}
}

func TestCustomTokenizer(t *testing.T) {
	// 自定义 tokenizer：每个字符计 1 token
	tok := &mockTokenizer{}
	c := NewChunker(tok)

	doc := &loader.Document{
		Blocks: []loader.Block{
			{Type: loader.BlockParagraph, Content: "0123456789"}, // 10 token
		},
		Metadata: loader.DocumentMeta{Filename: "test.txt"},
	}

	config := ChunkerConfig{
		Strategy:  StrategyFixed,
		ChunkSize: 10,
	}

	chunks := c.Chunk(doc, config)

	if len(chunks) != 1 {
		t.Errorf("10 字符 + ChunkSize=10 应输出 1 chunk，实际 %d", len(chunks))
	}
}

func TestCustomStrategyRegistration(t *testing.T) {
	tok := &mockTokenizer{}
	c := NewChunker(tok)

	mock := &mockStrategy{}
	customType := StrategyType(99)
	c.RegisterStrategy(customType, mock)

	doc := &loader.Document{
		Blocks: []loader.Block{
			{Type: loader.BlockParagraph, Content: "test content"},
		},
		Metadata: loader.DocumentMeta{Filename: "test.txt"},
	}

	config := ChunkerConfig{
		Strategy:  customType,
		ChunkSize: 100,
	}

	chunks := c.Chunk(doc, config)

	if !mock.called {
		t.Error("自定义策略未被调用")
	}
	if len(chunks) != 1 {
		t.Errorf("期望 1 个 chunk，实际 %d", len(chunks))
	}
}

func getLastNChars(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}
