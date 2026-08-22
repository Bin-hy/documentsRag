package loader

import (
	"strings"
	"unicode"
)

// pdfInstructionWords PDF 内容流操作符/指令词：这些是 PDF 语法命令，非人类可读文字，不计入可读量。
var pdfInstructionWords = map[string]bool{
	"q": true, "Q": true, "cm": true, "re": true, "w": true, "W": true, "n": true,
	"f": true, "f*": true, "B": true, "b": true, "s": true, "S": true, "b*": true, "B*": true,
	"g": true, "G": true, "rg": true, "RG": true, "k": true, "K": true, "sc": true, "scn": true,
	"BT": true, "ET": true, "Tf": true, "TJ": true, "Tj": true, "Tc": true, "Tw": true,
	"Tz": true, "TL": true, "Tm": true, "Td": true, "TD": true, "T*": true, "'": true,
	"BDC": true, "EMC": true, "BMC": true, "MP": true, "DP": true,
	"Do": true, "ID": true, "BI": true, "EI": true, "gs": true, "sh": true,
	"Im": true, "DC": true, "BT2": true, "ET2": true,
}

// ReadableCharCount 统计文本中的可读文本量：
// 可读内容 = 中文字符数 + 真实单词数。
// 真实单词 = 纯字母串（仅 A-Za-z）且长度 2-20，且不是 PDF 指令词（cm/TJ/Do/Im 等）：
//   - 排除 PDF 十六进制编码文本（如 "<2FB12192302B08D8>" 含数字，是 CID 字体编码非人类文字）
//   - 排除 PDF 内容流操作符（BT/Tf/TJ/cm/re/Do/Im 等）
//   - 真实英文单词（plugin/document/project 等）正常计入
func ReadableCharCount(text string) int {
	count := 0
	word := strings.Builder{}
	flush := func() {
		n := word.Len()
		if n >= 2 && n <= 20 && !pdfInstructionWords[word.String()] {
			count++
		}
		word.Reset()
	}
	for _, r := range text {
		if r == '\uFFFD' {
			flush()
			continue
		}
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			count++ // 每个汉字计 1
		case r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
			word.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return count
}

// ValidateReadable 校验文档是否含可读文本。
// minChars <= 0 表示禁用判定（返回 nil）；文档为 nil、无 Blocks 时拒绝。
// 双条件判定（防「大量低密度碎片累加」绕过）：
//  1. 全文可读文本总量 ≥ minChars
//  2. 至少一个 block 的可读文本量 ≥ minChars（真实文档的实质内容 block 单个即达标；
//     PDF 内容流指令碎片（BT/Tf/TJ/re）每个 block 仅 5-15 字，无法满足）
//
// 任一不满足返回 *ErrNoReadableContent。
func ValidateReadable(doc *Document, minChars int) error {
	if minChars <= 0 {
		return nil
	}
	if doc == nil {
		return &ErrNoReadableContent{Format: "", Readable: 0, MinChars: minChars}
	}

	// 多媒体（音频/图片/视频）转写文本按时间分段产出，每个 block 可能仅数字符，
	// 不适用「单块 ≥ minChars」的 PDF 防碎片绕过判定。各媒体解析器已各自保证非空产出，
	// 且上传预检已对媒体豁免可读性校验；此处与上传侧保持一致，避免有效转写在入库阶段被误拒。
	switch doc.Metadata.Format {
	case "audio", "image", "video":
		return nil
	}

	total := 0
	maxBlock := 0
	for _, b := range doc.Blocks {
		n := ReadableCharCount(b.Content)
		total += n
		if n > maxBlock {
			maxBlock = n
		}
	}
	if total < minChars || maxBlock < minChars {
		return &ErrNoReadableContent{
			Format:   doc.Metadata.Format,
			Readable: maxBlock, // 展示代表性 block 的可读量
			MinChars: minChars,
		}
	}
	return nil
}
