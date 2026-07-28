package textformatter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractorDecodesSupportedTextEncodings(t *testing.T) {
	extractor := NewExtractor(Config{})
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "utf8", data: []byte("中文\nEnglish"), want: "中文\nEnglish"},
		{name: "utf8 bom", data: append([]byte{0xef, 0xbb, 0xbf}, []byte("中文")...), want: "中文"},
		{name: "gb18030", data: []byte{0xd6, 0xd0, 0xce, 0xc4}, want: "中文"},
		{name: "utf16 le", data: utf16Bytes(binary.LittleEndian, []uint16{0xfeff, '中', '文'}), want: "中文"},
		{name: "utf16 be", data: utf16Bytes(binary.BigEndian, []uint16{0xfeff, '中', '文'}), want: "中文"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractor.Extract(context.Background(), "article.txt", "text/plain", tt.data)
			require.NoError(t, err)
			require.Equal(t, tt.want, result.Text)
			require.Equal(t, "txt", result.Format)
		})
	}
}

func TestExtractorPreservesMarkdownSourceExactly(t *testing.T) {
	content := "# 标题\n\n- 列表\n\n```go\nfmt.Println(\"正文\")\n```\n\n[链接](https://example.com)"
	for _, fileName := range []string{"article.md", "article.markdown"} {
		result, err := NewExtractor(Config{}).Extract(context.Background(), fileName, "text/markdown; charset=utf-8", []byte(content))
		require.NoError(t, err)
		require.Equal(t, content, result.Text)
	}
}

func TestExtractorReadsAllSupportedDOCXTextParts(t *testing.T) {
	data := makeDOCX(t, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="w"><w:body>
			<w:p><w:r><w:t>正文标题</w:t></w:r></w:p>
			<w:p><w:hyperlink><w:r><w:t>链接文字</w:t></w:r></w:hyperlink></w:p>
			<w:tbl><w:tr><w:tc><w:p><w:r><w:t>表格甲</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>表格乙</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
			<w:p><w:r><w:t>文本框文字</w:t></w:r></w:p>
		</w:body></w:document>`,
		"word/header1.xml":   `<?xml version="1.0"?><w:hdr xmlns:w="w"><w:p><w:r><w:t>页眉文字</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":   `<?xml version="1.0"?><w:ftr xmlns:w="w"><w:p><w:r><w:t>页脚文字</w:t></w:r></w:p></w:ftr>`,
		"word/footnotes.xml": `<?xml version="1.0"?><w:footnotes xmlns:w="w"><w:footnote><w:p><w:r><w:t>脚注文字</w:t></w:r></w:p></w:footnote></w:footnotes>`,
		"word/endnotes.xml":  `<?xml version="1.0"?><w:endnotes xmlns:w="w"><w:endnote><w:p><w:r><w:t>尾注文字</w:t></w:r></w:p></w:endnote></w:endnotes>`,
		"word/comments.xml":  `<?xml version="1.0"?><w:comments xmlns:w="w"><w:comment><w:p><w:r><w:t>批注文字</w:t></w:r></w:p></w:comment></w:comments>`,
	})

	result, err := NewExtractor(Config{}).Extract(context.Background(), "article.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data)
	require.NoError(t, err)
	require.Equal(t, "docx", result.Format)
	require.Equal(t, "正文标题\n链接文字\n表格甲\n\t表格乙\n\t文本框文字\n页眉文字\n页脚文字\n脚注文字\n尾注文字\n批注文字\n", result.Text)
}

func TestExtractorSkipsDeletedDOCXRevisionText(t *testing.T) {
	data := makeDOCX(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="w"><w:body><w:p><w:r><w:t>保留文字</w:t></w:r><w:del><w:r><w:delText>删除文字</w:delText></w:r></w:del><w:moveFrom><w:r><w:t>移动前文字</w:t></w:r></w:moveFrom><w:moveTo><w:r><w:t>移动后文字</w:t></w:r></w:moveTo></w:p></w:body></w:document>`,
	})

	result, err := NewExtractor(Config{}).Extract(context.Background(), "article.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data)
	require.NoError(t, err)
	require.Contains(t, result.Text, "保留文字")
	require.NotContains(t, result.Text, "删除文字")
	require.NotContains(t, result.Text, "移动前文字")
	require.Contains(t, result.Text, "移动后文字")
}

func TestExtractorUsesOneDOCXAlternateContentBranch(t *testing.T) {
	data := makeDOCX(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="w" xmlns:mc="mc"><w:body><mc:AlternateContent><mc:Choice><w:p><w:r><w:t>重复文本框</w:t></w:r></w:p></mc:Choice><mc:Fallback><w:p><w:r><w:t>重复文本框</w:t></w:r></w:p></mc:Fallback></mc:AlternateContent></w:body></w:document>`,
	})

	result, err := NewExtractor(Config{}).Extract(context.Background(), "article.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(result.Text, "重复文本框"))
}

func TestExtractorReturnsDeterministicDOCXText(t *testing.T) {
	data := makeDOCX(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="w"><w:body><w:p><w:r><w:t>正文</w:t></w:r></w:p></w:body></w:document>`,
		"word/header1.xml":  `<?xml version="1.0"?><w:hdr xmlns:w="w"><w:p><w:r><w:t>页眉</w:t></w:r></w:p></w:hdr>`,
	})

	result, err := NewExtractor(Config{}).Extract(context.Background(), "article.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data)
	require.NoError(t, err)
	require.Equal(t, "正文\n页眉\n", result.Text)
}

func TestExtractorRejectsIncompleteDOCX(t *testing.T) {
	data := makeDOCX(t, map[string]string{"word/header1.xml": `<w:hdr xmlns:w="w"><w:p><w:r><w:t>只有页眉</w:t></w:r></w:p></w:hdr>`})
	_, err := NewExtractor(Config{}).Extract(context.Background(), "article.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data)
	require.ErrorContains(t, err, "document.xml")
}

func TestExtractorRejectsZIPThatIsNotADOCXPackage(t *testing.T) {
	data := makeZIP(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"word/document.xml":   `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>伪装正文</w:t></w:r></w:p></w:body></w:document>`,
	})

	_, err := NewExtractor(Config{}).Extract(context.Background(), "fake.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data)
	require.ErrorContains(t, err, "content type")
}

func TestExtractorRejectsEncryptedDOCXContainerClearly(t *testing.T) {
	data := append(append([]byte{}, oleMagic...), bytes.Repeat([]byte{0}, 32)...)

	_, err := NewExtractor(Config{}).Extract(context.Background(), "encrypted.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data)
	require.ErrorContains(t, err, "encrypted")
}

func TestExtractorUsesOfflineAntiwordForLegacyDOC(t *testing.T) {
	var called bool
	extractor := NewExtractor(Config{
		TempDir: t.TempDir(),
		RunAntiword: func(_ context.Context, path string) ([]byte, error) {
			called = true
			stored, err := os.ReadFile(path)
			require.NoError(t, err)
			require.True(t, bytes.HasPrefix(stored, oleMagic))
			return []byte("旧版正文\n表格内容"), nil
		},
	})
	data := append(append([]byte{}, oleMagic...), bytes.Repeat([]byte{0}, 32)...)

	result, err := extractor.Extract(context.Background(), "legacy.doc", "application/msword", data)
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, "旧版正文\n表格内容", result.Text)
	require.Equal(t, "doc", result.Format)
}

func TestExtractorRejectsMismatchedAndUnsupportedFiles(t *testing.T) {
	extractor := NewExtractor(Config{})
	_, err := extractor.Extract(context.Background(), "fake.docx", "application/octet-stream", []byte("not a zip"))
	require.ErrorContains(t, err, "DOCX")

	_, err = extractor.Extract(context.Background(), "article.pdf", "application/pdf", []byte("pdf"))
	require.ErrorContains(t, err, "unsupported")

	docx := makeDOCX(t, map[string]string{"word/document.xml": `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>正文</w:t></w:r></w:p></w:body></w:document>`})
	_, err = extractor.Extract(context.Background(), "article.docx", "application/msword", docx)
	require.ErrorContains(t, err, "content type")

	doc := append(append([]byte{}, oleMagic...), bytes.Repeat([]byte{0}, 32)...)
	_, err = extractor.Extract(context.Background(), "article.doc", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", doc)
	require.ErrorContains(t, err, "content type")

	_, err = extractor.Extract(context.Background(), "renamed.txt", "text/plain", docx)
	require.ErrorContains(t, err, "signature")

	_, err = extractor.Extract(context.Background(), "renamed.txt", "text/plain", []byte("%PDF-1.7\nnot text"))
	require.ErrorContains(t, err, "signature")
}

func TestExtractorRejectsEmptyTextDocument(t *testing.T) {
	_, err := NewExtractor(Config{}).Extract(context.Background(), "empty.txt", "text/plain", nil)
	require.ErrorContains(t, err, "no readable text")
}

func utf16Bytes(order binary.ByteOrder, values []uint16) []byte {
	output := make([]byte, len(values)*2)
	for index, value := range values {
		order.PutUint16(output[index*2:], value)
	}
	return output
}

func makeDOCX(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	parts["[Content_Types].xml"] = `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`
	return makeZIP(t, parts)
}

func makeZIP(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, content := range parts {
		part, err := writer.Create(filepath.ToSlash(name))
		require.NoError(t, err)
		_, err = part.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return output.Bytes()
}
