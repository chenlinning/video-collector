package textformatter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const maxExpandedDOCXBytes int64 = 64 << 20

const docxMainContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"

var oleMagic = []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}

type AntiwordRunner func(context.Context, string) ([]byte, error)

type Config struct {
	AntiwordPath string
	TempDir      string
	RunAntiword  AntiwordRunner
}

type Document struct {
	FileName       string `json:"fileName"`
	Format         string `json:"format"`
	Text           string `json:"text"`
	ByteSize       int    `json:"byteSize"`
	CharacterCount int    `json:"characterCount"`
}

type Extractor struct {
	antiwordPath string
	tempDir      string
	runAntiword  AntiwordRunner
}

func NewExtractor(config Config) *Extractor {
	if strings.TrimSpace(config.AntiwordPath) == "" {
		config.AntiwordPath = "/usr/bin/antiword"
	}
	extractor := &Extractor{antiwordPath: config.AntiwordPath, tempDir: config.TempDir, runAntiword: config.RunAntiword}
	if extractor.runAntiword == nil {
		extractor.runAntiword = extractor.runAntiwordCommand
	}
	return extractor
}

func (e *Extractor) Extract(ctx context.Context, fileName, contentType string, data []byte) (Document, error) {
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "" || fileName == "." {
		return Document{}, errors.New("document file name is required")
	}
	extension := strings.ToLower(filepath.Ext(fileName))
	if !supportedExtension(extension) {
		return Document{}, fmt.Errorf("unsupported document format %q", extension)
	}
	if !validDocumentContentType(extension, contentType) {
		return Document{}, errors.New("document content type does not match its extension")
	}
	var (
		text   string
		format string
		err    error
	)
	switch extension {
	case ".txt", ".md", ".markdown":
		if hasNonTextSignature(data) {
			return Document{}, errors.New("text file signature does not match its extension")
		}
		text, err = decodeText(data)
		format = strings.TrimPrefix(extension, ".")
	case ".docx":
		if bytes.HasPrefix(data, oleMagic) {
			return Document{}, errors.New("encrypted DOCX containers are not supported")
		}
		if !isZip(data) {
			return Document{}, errors.New("DOCX file signature is invalid")
		}
		text, err = extractDOCX(data)
		format = "docx"
	case ".doc":
		if !bytes.HasPrefix(data, oleMagic) {
			return Document{}, errors.New("DOC file signature is invalid")
		}
		text, err = e.extractDOC(ctx, data)
		format = "doc"
	}
	if err != nil {
		return Document{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Document{}, errors.New("document contains no readable text")
	}
	return Document{
		FileName: fileName, Format: format, Text: text, ByteSize: len(data), CharacterCount: utf8.RuneCountInString(text),
	}, nil
}

func hasNonTextSignature(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return isZip(data) || bytes.HasPrefix(data, oleMagic) || bytes.HasPrefix(trimmed, []byte("%PDF-"))
}

func supportedExtension(extension string) bool {
	switch extension {
	case ".txt", ".md", ".markdown", ".docx", ".doc":
		return true
	default:
		return false
	}
}

func validDocumentContentType(extension, value string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/octet-stream" {
		return true
	}
	switch extension {
	case ".txt", ".md", ".markdown":
		return mediaType == "text/plain" || mediaType == "text/markdown" || mediaType == "text/x-markdown"
	case ".docx":
		return mediaType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".doc":
		return mediaType == "application/msword"
	default:
		return false
	}
}

func decodeText(data []byte) (string, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		return decodeUTF8(data[3:])
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		return decodeUTF16(data[2:], binary.LittleEndian)
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		return decodeUTF16(data[2:], binary.BigEndian)
	case utf8.Valid(data):
		return string(data), nil
	default:
		decoded, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), data)
		if err != nil || !utf8.Valid(decoded) {
			return "", errors.New("text file encoding is not supported")
		}
		return string(decoded), nil
	}
}

func decodeUTF8(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", errors.New("UTF-8 text is invalid")
	}
	return string(data), nil
}

func decodeUTF16(data []byte, order binary.ByteOrder) (string, error) {
	if len(data)%2 != 0 {
		return "", errors.New("UTF-16 text has an incomplete code unit")
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = order.Uint16(data[index*2:])
	}
	return string(utf16.Decode(units)), nil
}

func isZip(data []byte) bool {
	return len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && (data[2] == 3 || data[2] == 5 || data[2] == 7) && (data[3] == 4 || data[3] == 6 || data[3] == 8)
}

func extractDOCX(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errors.New("DOCX archive cannot be opened")
	}
	parts := make(map[string]*zip.File, len(reader.File))
	var expanded int64
	for _, file := range reader.File {
		expanded += int64(file.UncompressedSize64)
		if expanded > maxExpandedDOCXBytes {
			return "", errors.New("DOCX expanded content is too large")
		}
		parts[filepath.ToSlash(file.Name)] = file
	}
	if _, encrypted := parts["EncryptedPackage"]; encrypted {
		return "", errors.New("encrypted Word documents are not supported")
	}
	contentTypes, ok := parts["[Content_Types].xml"]
	if !ok {
		return "", errors.New("DOCX is missing [Content_Types].xml")
	}
	if _, ok := parts["word/document.xml"]; !ok {
		return "", errors.New("DOCX is missing word/document.xml")
	}
	if err := validateDOCXContentTypes(contentTypes); err != nil {
		return "", err
	}

	ordered := []string{"word/document.xml"}
	ordered = append(ordered, matchingPartNames(parts, "word/header", ".xml")...)
	ordered = append(ordered, matchingPartNames(parts, "word/footer", ".xml")...)
	for _, name := range []string{"word/footnotes.xml", "word/endnotes.xml", "word/comments.xml"} {
		if _, ok := parts[name]; ok {
			ordered = append(ordered, name)
		}
	}

	var output strings.Builder
	for _, name := range ordered {
		text, err := extractXMLText(parts[name])
		if err != nil {
			return "", fmt.Errorf("DOCX part %s cannot be read: %w", name, err)
		}
		if text == "" {
			continue
		}
		if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n") {
			output.WriteByte('\n')
		}
		output.WriteString(text)
	}
	return output.String(), nil
}

func validateDOCXContentTypes(file *zip.File) error {
	reader, err := file.Open()
	if err != nil {
		return errors.New("DOCX content types cannot be opened")
	}
	defer reader.Close()
	decoder := xml.NewDecoder(io.LimitReader(reader, maxExpandedDOCXBytes))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("DOCX content types cannot be read")
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Override" {
			continue
		}
		var partName, contentType string
		for _, attribute := range start.Attr {
			switch attribute.Name.Local {
			case "PartName":
				partName = attribute.Value
			case "ContentType":
				contentType = attribute.Value
			}
		}
		if partName == "/word/document.xml" {
			if contentType != docxMainContentType {
				return errors.New("DOCX main document content type is invalid")
			}
			return nil
		}
	}
	return errors.New("DOCX main document content type is missing")
}

func matchingPartNames(parts map[string]*zip.File, prefix, suffix string) []string {
	names := make([]string, 0)
	for name := range parts {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func extractXMLText(file *zip.File) (string, error) {
	reader, err := file.Open()
	if err != nil {
		return "", err
	}
	defer reader.Close()
	decoder := xml.NewDecoder(io.LimitReader(reader, maxExpandedDOCXBytes))
	decoder.Strict = true
	var output strings.Builder
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "del", "moveFrom", "Choice":
				if err := decoder.Skip(); err != nil {
					return "", err
				}
			case "t":
				var text string
				if err := decoder.DecodeElement(&text, &value); err != nil {
					return "", err
				}
				output.WriteString(text)
			case "tab":
				output.WriteByte('\t')
			case "br", "cr":
				output.WriteByte('\n')
			}
		case xml.EndElement:
			switch value.Name.Local {
			case "p":
				output.WriteByte('\n')
			case "tc":
				output.WriteByte('\t')
			}
		}
	}
	return output.String(), nil
}

func (e *Extractor) extractDOC(ctx context.Context, data []byte) (string, error) {
	file, err := os.CreateTemp(e.tempDir, "video-collector-text-*.doc")
	if err != nil {
		return "", errors.New("temporary Word file cannot be created")
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return "", errors.New("temporary Word file cannot be written")
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	output, err := e.runAntiword(ctx, path)
	if err != nil {
		return "", fmt.Errorf("legacy Word document cannot be extracted: %w", err)
	}
	if !utf8.Valid(output) {
		return "", errors.New("legacy Word extractor returned invalid UTF-8")
	}
	return string(output), nil
}

func (e *Extractor) runAntiwordCommand(ctx context.Context, path string) ([]byte, error) {
	commandContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, e.antiwordPath, "-w", "0", "-m", "UTF-8.txt", path)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 200 {
			message = message[:200]
		}
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return output, nil
}
