package retriever

import (
	"strings"
	"testing"
)

// TestTokenizerCJKUnigram 验证开启 unigram 后中文同时输出单字与 bigram
func TestTokenizerCJKUnigram(t *testing.T) {
	tk := NewSimpleTokenizer(WithCJKUnigram())
	tokens := tk.Tokenize("数据库")
	// 期望: unigram 数/据/库 + bigram 数据/据库
	expected := []string{"数", "据", "库", "数据", "据库"}
	if len(tokens) != len(expected) {
		t.Fatalf("期望 %d 个 token，实际 %d: %v", len(expected), len(tokens), tokens)
	}
	for i, e := range expected {
		if tokens[i] != e {
			t.Errorf("第 %d 个 token 期望 %q，实际 %q", i, e, tokens[i])
		}
	}
}

// TestTokenizerCJKUnigramRecall 验证单字查询 token 能命中索引词
func TestTokenizerCJKUnigramRecall(t *testing.T) {
	tk := NewSimpleTokenizer(WithCJKUnigram())
	docTokens := make(map[string]struct{})
	for _, tok := range tk.Tokenize("向量数据库") {
		docTokens[tok] = struct{}{}
	}
	for _, q := range tk.Tokenize("库") {
		if _, ok := docTokens[q]; !ok {
			t.Errorf("查询 token %q 未命中文档 token 集合", q)
		}
	}
}

// TestTokenizerStopWords 验证英文停用词过滤
func TestTokenizerStopWords(t *testing.T) {
	tk := NewSimpleTokenizer(WithEnglishStopWords("the", "IS"))
	tokens := tk.Tokenize("The cat IS cute")
	expected := []string{"cat", "cute"}
	if len(tokens) != len(expected) {
		t.Fatalf("期望 %d 个 token，实际 %d: %v", len(expected), len(tokens), tokens)
	}
	for i, e := range expected {
		if tokens[i] != e {
			t.Errorf("第 %d 个 token 期望 %q，实际 %q", i, e, tokens[i])
		}
	}
}

// TestTokenizerEmpty 验证空输入与纯边界字符输入
func TestTokenizerEmpty(t *testing.T) {
	tk := NewSimpleTokenizer()
	if tokens := tk.Tokenize(""); len(tokens) != 0 {
		t.Errorf("空字符串应返回 0 个 token，实际: %v", tokens)
	}
	if tokens := tk.Tokenize("   ！？。， "); len(tokens) != 0 {
		t.Errorf("纯标点空白应返回 0 个 token，实际: %v", tokens)
	}
}

// TestTokenizerExtHan 验证扩展区汉字（超出 U+9FFF）仍能按汉字处理
func TestTokenizerExtHan(t *testing.T) {
	tk := NewSimpleTokenizer()
	// "𠀀" 在 CJK 扩展 B 区（U+20000+）
	tokens := tk.Tokenize("𠀀文")
	if len(tokens) != 1 || tokens[0] != "𠀀文" {
		t.Errorf("扩展区汉字 bigram 错误: %v", tokens)
	}
}

// TestTokenizerDigitMixed 验证数字与字母混合聚合
func TestTokenizerDigitMixed(t *testing.T) {
	tk := NewSimpleTokenizer()
	tokens := tk.Tokenize("BM25算法v2")
	expected := []string{"bm25", "算法", "v2"}
	if strings.Join(tokens, ",") != strings.Join(expected, ",") {
		t.Errorf("期望 %v，实际 %v", expected, tokens)
	}
}
