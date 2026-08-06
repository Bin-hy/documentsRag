package loader

import "fmt"

// ErrUnsupportedFormat 不支持的文件格式
type ErrUnsupportedFormat struct {
	Filename string
	MIMEType string
}

func (e *ErrUnsupportedFormat) Error() string {
	if e.MIMEType != "" {
		return fmt.Sprintf("不支持的文件格式: %s (MIME: %s)", e.Filename, e.MIMEType)
	}
	return fmt.Sprintf("不支持的文件格式: %s", e.Filename)
}

// ErrParseFailed 解析失败
type ErrParseFailed struct {
	Format string
	Cause  error
}

func (e *ErrParseFailed) Error() string {
	return fmt.Sprintf("解析 %s 文件失败: %v", e.Format, e.Cause)
}

func (e *ErrParseFailed) Unwrap() error {
	return e.Cause
}

// ErrNoReadableContent 文档无可读文本（扫描件/空文件/纯乱码），拒绝入库
type ErrNoReadableContent struct {
	Format   string // 文件格式（pdf/txt/...）
	Readable int    // 实际可读字符数
	MinChars int    // 最低阈值
}

func (e *ErrNoReadableContent) Error() string {
	return fmt.Sprintf("文档无可读文本（扫描件或内容为空），可读字符 %d/最低 %d，不支持解析入库",
		e.Readable, e.MinChars)
}
