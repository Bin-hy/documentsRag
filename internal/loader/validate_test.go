package loader

import (
	"strings"
	"testing"
)

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestReadableCharCount(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"正常中文", "支持多种文档格式解析", 10},
		{"中文+英文+数字", "BinRag 支持 5 种格式", 6}, // 中文5 + 单词BinRag 1 = 6
		{"空白不计", "   \n\t  ", 0},
		{"替换符断开单词", "ab\uFFFDef", 2}, // ab 与 ef 各为单词
		{"单词计数", "hello world test", 3},
		{"单字母不计", "q g a", 0},
		{"命令词不计入", "q 595.44 0 0 841.68 cm 1 g /Im10 Do Q", 0}, // cm/Im/Do 均是指令词
		{"空串", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ReadableCharCount(c.text); got != c.want {
				t.Errorf("ReadableCharCount(%q) = %d, want %d", c.text, got, c.want)
			}
		})
	}
}

func TestValidateReadable(t *testing.T) {
	t.Run("正常文本通过", func(t *testing.T) {
		doc := &Document{Metadata: DocumentMeta{Format: "txt"},
			Blocks: []Block{{Type: BlockParagraph, Content: "这是一段正常的中文文档内容，用于测试解析入库是否正常。"}}}
		if err := ValidateReadable(doc, 20); err != nil {
			t.Errorf("正常文本应通过: %v", err)
		}
	})

	t.Run("图像指令乱码被拒", func(t *testing.T) {
		doc := &Document{Metadata: DocumentMeta{Format: "pdf"},
			Blocks: []Block{{Type: BlockParagraph,
				Content: "q 595.44 0 0 841.68 0.00 0.00 cm 1 g /Im10 Do Q\nq 595.44 0 0 841.68 0.00 0.00 cm 1 g /Im11 Do Q"}}}
		err := ValidateReadable(doc, 20)
		if err == nil {
			t.Fatal("乱码应被拒绝")
		}
		nerr, ok := err.(*ErrNoReadableContent)
		if !ok {
			t.Fatalf("错误类型应为 ErrNoReadableContent，实际 %T", err)
		}
		if nerr.Format != "pdf" || nerr.Readable >= nerr.MinChars {
			t.Errorf("错误字段错误: %+v", nerr)
		}
	})

	t.Run("空 Blocks 被拒", func(t *testing.T) {
		doc := &Document{Metadata: DocumentMeta{Format: "pdf"}, Blocks: nil}
		if err := ValidateReadable(doc, 20); err == nil {
			t.Fatal("空文档应被拒绝")
		}
	})

	t.Run("nil 文档被拒", func(t *testing.T) {
		if err := ValidateReadable(nil, 20); err == nil {
			t.Fatal("nil 文档应被拒绝")
		}
	})

	t.Run("minChars=0 禁用判定", func(t *testing.T) {
		doc := &Document{Metadata: DocumentMeta{Format: "pdf"},
			Blocks: []Block{{Type: BlockParagraph, Content: "q 1 0 0 1 cm /Im Do Q"}}}
		if err := ValidateReadable(doc, 0); err != nil {
			t.Errorf("minChars=0 应跳过判定: %v", err)
		}
	})

	t.Run("错误文案含原因", func(t *testing.T) {
		doc := &Document{Metadata: DocumentMeta{Format: "pdf"},
			Blocks: []Block{{Type: BlockParagraph, Content: "q 1 0 0 1 cm"}}}
		err := ValidateReadable(doc, 20)
		if err == nil {
			t.Fatal("应报错")
		}
		msg := err.Error()
		if !containsAny(msg, "无可读文本", "不支持解析入库", "最低 20") {
			t.Errorf("文案缺少关键信息: %q", msg)
		}
	})
}

func TestValidateReadableFragments(t *testing.T) {
	// 多个低密度碎片累加（总量可能超阈值），但无单个 block 达标 → 应拒绝
	doc := &Document{Metadata: DocumentMeta{Format: "pdf"}, Blocks: []Block{
		{Type: BlockParagraph, Content: "q 595.44 0 0 841.68 cm 1 g /Im10 Do Q"},
		{Type: BlockParagraph, Content: "BT /F7 10.56 Tf 1 0 0 1 352.63 183.02 Tm [( )] TJ ET"},
		{Type: BlockParagraph, Content: "0.196 RG -0.936 Tc[( )] TJ ET Q"},
		{Type: BlockParagraph, Content: "0.000008871 0 595.32 841.92 re W* n"},
	}}
	err := ValidateReadable(doc, 20)
	if err == nil {
		t.Fatal("低密度碎片累加应被拒绝")
	}
	nerr, ok := err.(*ErrNoReadableContent)
	if !ok {
		t.Fatalf("错误类型错误: %T", err)
	}
	t.Logf("碎片文档可读量(最大block)=%d", nerr.Readable)
}

func TestValidateReadableMultiBlockPass(t *testing.T) {
	// 多 block 且至少一个达标 → 通过
	doc := &Document{Metadata: DocumentMeta{Format: "pdf"}, Blocks: []Block{
		{Type: BlockHeading, Content: "标题"},
		{Type: BlockParagraph, Content: "这是正文内容，包含足够多的可读文字用于通过校验阈值，说明文档是有效的文本型文档。"},
	}}
	if err := ValidateReadable(doc, 20); err != nil {
		t.Errorf("含达标 block 的文档应通过: %v", err)
	}
}
