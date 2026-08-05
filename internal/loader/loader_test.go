package loader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestTxtParser(t *testing.T) {
	input := "第一段内容\n这还是第一段\n\n第二段内容\n\n第三段内容"
	reader := strings.NewReader(input)

	l := NewLoader()
	result, err := l.Load(context.Background(), reader, FileInfo{Filename: "test.txt", Size: int64(len(input))})
	if err != nil {
		t.Fatalf("加载 TXT 失败: %v", err)
	}

	if len(result.Document.Blocks) != 3 {
		t.Fatalf("期望 3 个 Block，实际 %d", len(result.Document.Blocks))
	}

	for _, b := range result.Document.Blocks {
		if b.Type != BlockParagraph {
			t.Errorf("期望 BlockParagraph，实际 %d", b.Type)
		}
	}

	if result.Document.Metadata.Filename != "test.txt" {
		t.Errorf("Filename 期望 test.txt，实际 %s", result.Document.Metadata.Filename)
	}
	if result.Document.Metadata.Format != "txt" {
		t.Errorf("Format 期望 txt，实际 %s", result.Document.Metadata.Format)
	}
}

func TestMarkdownParser(t *testing.T) {
	input := `# 主标题

这是正文段落。

## 二级标题

- 列表项1
- 列表项2

### 三级标题

` + "```go\nfmt.Println(\"hello\")\n```"

	reader := strings.NewReader(input)

	l := NewLoader()
	result, err := l.Load(context.Background(), reader, FileInfo{Filename: "test.md"})
	if err != nil {
		t.Fatalf("加载 Markdown 失败: %v", err)
	}

	doc := result.Document
	if doc.Metadata.Title != "主标题" {
		t.Errorf("Title 期望 '主标题'，实际 '%s'", doc.Metadata.Title)
	}

	// 验证标题层级
	headings := filterBlocks(doc.Blocks, BlockHeading)
	if len(headings) < 3 {
		t.Fatalf("期望至少 3 个 Heading，实际 %d", len(headings))
	}
	if headings[0].Level != 1 {
		t.Errorf("第一个标题期望 Level=1，实际 %d", headings[0].Level)
	}
	if headings[1].Level != 2 {
		t.Errorf("第二个标题期望 Level=2，实际 %d", headings[1].Level)
	}
	if headings[2].Level != 3 {
		t.Errorf("第三个标题期望 Level=3，实际 %d", headings[2].Level)
	}

	// 验证段落
	paragraphs := filterBlocks(doc.Blocks, BlockParagraph)
	if len(paragraphs) < 1 {
		t.Error("期望至少 1 个段落")
	}

	// 验证列表项
	listItems := filterBlocks(doc.Blocks, BlockListItem)
	if len(listItems) != 2 {
		t.Errorf("期望 2 个列表项，实际 %d", len(listItems))
	}

	// 验证代码块
	codeBlocks := filterBlocks(doc.Blocks, BlockCode)
	if len(codeBlocks) != 1 {
		t.Errorf("期望 1 个代码块，实际 %d", len(codeBlocks))
	}
}

func TestCsvParser(t *testing.T) {
	input := "姓名,年龄,城市\n张三,25,北京\n李四,30,上海"
	reader := strings.NewReader(input)

	l := NewLoader()
	result, err := l.Load(context.Background(), reader, FileInfo{Filename: "data.csv"})
	if err != nil {
		t.Fatalf("加载 CSV 失败: %v", err)
	}

	doc := result.Document
	if len(doc.Blocks) != 3 {
		t.Fatalf("期望 3 个 Block（1 表头 + 2 数据），实际 %d", len(doc.Blocks))
	}

	if doc.Blocks[0].Type != BlockHeading {
		t.Error("第一个 Block 期望为 BlockHeading")
	}
	if doc.Blocks[1].Type != BlockTable {
		t.Error("第二个 Block 期望为 BlockTable")
	}
	if doc.Blocks[2].Type != BlockTable {
		t.Error("第三个 Block 期望为 BlockTable")
	}
}

func TestExcelParser(t *testing.T) {
	// 用 excelize 在内存中创建 xlsx
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "数据表")
	f.SetCellValue("数据表", "A1", "姓名")
	f.SetCellValue("数据表", "B1", "分数")
	f.SetCellValue("数据表", "A2", "小明")
	f.SetCellValue("数据表", "B2", "95")

	idx, _ := f.NewSheet("汇总")
	_ = idx
	f.SetCellValue("汇总", "A1", "总计")

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("创建测试 xlsx 失败: %v", err)
	}

	l := NewLoader()
	result, err := l.Load(context.Background(), &buf, FileInfo{Filename: "test.xlsx"})
	if err != nil {
		t.Fatalf("加载 Excel 失败: %v", err)
	}

	doc := result.Document
	headings := filterBlocks(doc.Blocks, BlockHeading)
	if len(headings) < 2 {
		t.Errorf("期望至少 2 个 Sheet Heading，实际 %d", len(headings))
	}

	tableBlocks := filterBlocks(doc.Blocks, BlockTable)
	if len(tableBlocks) < 2 {
		t.Errorf("期望至少 2 个 Table Block，实际 %d", len(tableBlocks))
	}
}

func TestHtmlParser(t *testing.T) {
	input := `<html><body>
<h1>页面标题</h1>
<p>这是段落。</p>
<h2>二级标题</h2>
<ul><li>项目1</li><li>项目2</li></ul>
<pre><code>console.log("hi")</code></pre>
</body></html>`

	reader := strings.NewReader(input)
	l := NewLoader()
	result, err := l.Load(context.Background(), reader, FileInfo{Filename: "page.html"})
	if err != nil {
		t.Fatalf("加载 HTML 失败: %v", err)
	}

	doc := result.Document
	if doc.Metadata.Title != "页面标题" {
		t.Errorf("Title 期望 '页面标题'，实际 '%s'", doc.Metadata.Title)
	}

	headings := filterBlocks(doc.Blocks, BlockHeading)
	if len(headings) < 2 {
		t.Errorf("期望至少 2 个 Heading，实际 %d", len(headings))
	}
	if headings[0].Level != 1 {
		t.Errorf("h1 Level 期望 1，实际 %d", headings[0].Level)
	}

	paragraphs := filterBlocks(doc.Blocks, BlockParagraph)
	if len(paragraphs) < 1 {
		t.Error("期望至少 1 个段落")
	}

	listItems := filterBlocks(doc.Blocks, BlockListItem)
	if len(listItems) != 2 {
		t.Errorf("期望 2 个列表项，实际 %d", len(listItems))
	}
}

func TestUnsupportedFormat(t *testing.T) {
	reader := strings.NewReader("some content")
	l := NewLoader()
	_, err := l.Load(context.Background(), reader, FileInfo{Filename: "data.xyz"})
	if err == nil {
		t.Fatal("期望返回错误，实际无错误")
	}

	var unsupported *ErrUnsupportedFormat
	if !errors.As(err, &unsupported) {
		t.Errorf("期望 ErrUnsupportedFormat，实际 %T", err)
	}
	if unsupported.Filename != "data.xyz" {
		t.Errorf("错误中的文件名期望 data.xyz，实际 %s", unsupported.Filename)
	}
}

func TestMIMEFallback(t *testing.T) {
	input := "纯文本内容"
	reader := strings.NewReader(input)
	l := NewLoader()
	result, err := l.Load(context.Background(), reader, FileInfo{
		Filename: "noext",
		MIMEType: "text/plain",
	})
	if err != nil {
		t.Fatalf("MIME 降级失败: %v", err)
	}
	if result.Document.Metadata.Format != "txt" {
		t.Errorf("Format 期望 txt，实际 %s", result.Document.Metadata.Format)
	}
}

func TestTolerantMode(t *testing.T) {
	// 传入损坏的 PDF 数据（非有效 PDF）
	reader := strings.NewReader("this is not a valid pdf")
	l := NewLoader()
	result, err := l.Load(context.Background(), reader, FileInfo{Filename: "bad.pdf"})
	if err != nil {
		t.Fatalf("宽容模式不应返回错误，实际: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("宽容模式期望有 Warnings")
	}
	if result.Document == nil {
		t.Error("宽容模式期望返回 Document（可能为空）")
	}
}

func TestStrictMode(t *testing.T) {
	reader := strings.NewReader("this is not a valid pdf")
	l := NewLoader()
	_, err := l.Load(context.Background(), reader, FileInfo{Filename: "bad.pdf"}, LoadOptions{Mode: ModeStrict})
	if err == nil {
		t.Fatal("严格模式期望返回错误")
	}
}

func TestCustomParserRegistration(t *testing.T) {
	r := NewRegistry()
	r.Register(NewTxtParser())
	r.Register(&mockParser{})

	l := NewLoaderWithRegistry(r)
	reader := strings.NewReader("custom content")
	result, err := l.Load(context.Background(), reader, FileInfo{Filename: "test.custom"})
	if err != nil {
		t.Fatalf("自定义 Parser 加载失败: %v", err)
	}
	if result.Document.Metadata.Format != "custom" {
		t.Errorf("Format 期望 custom，实际 %s", result.Document.Metadata.Format)
	}
}

// mockParser 自定义格式解析器
type mockParser struct{}

func (p *mockParser) SupportedExts() []string  { return []string{".custom"} }
func (p *mockParser) SupportedMIMEs() []string { return []string{"application/x-custom"} }
func (p *mockParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	data, _ := io.ReadAll(reader)
	return &LoadResult{
		Document: &Document{
			Blocks:   []Block{{Type: BlockParagraph, Content: string(data)}},
			Metadata: DocumentMeta{Format: "custom"},
		},
	}, nil
}

// 辅助函数
func filterBlocks(blocks []Block, blockType BlockType) []Block {
	var result []Block
	for _, b := range blocks {
		if b.Type == blockType {
			result = append(result, b)
		}
	}
	return result
}
