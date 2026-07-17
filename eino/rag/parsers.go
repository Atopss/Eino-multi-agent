package rag

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

type docxBody struct {
	XMLName    xml.Name `xml:"body"`
	Paragraphs []docxP  `xml:"p"`
}

type docxP struct {
	Texts []docxR `xml:"r"`
}

type docxR struct {
	Text string `xml:"t"`
}

// parseDocx 从 .docx 文件中提取纯文本
func parseDocx(filePath string) (string, error) {
	// .docx 本质是 zip 文件
	zipReader, err := zip.OpenReader(filePath)
	if err != nil {
		return "", err
	}
	defer zipReader.Close()

	// 找到 word/document.xml
	for _, file := range zipReader.File {
		if file.Name != "word/document.xml" {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()

		data, err := io.ReadAll(rc)
		if err != nil {
			return "", err
		}

		// 解析 XML
		var body docxBody
		if err := xml.Unmarshal(data, &body); err != nil {
			return "", err
		}

		// 提取所有文本
		var sb strings.Builder
		for _, p := range body.Paragraphs {
			for _, r := range p.Texts {
				sb.WriteString(r.Text)
			}
			sb.WriteString("\n")
		}

		return sb.String(), nil
	}

	return "", fmt.Errorf("docx 中未找到 word/document.xml")
}

// ============================================================
// PDF 文档解析（简单文本提取）
// ============================================================

// parsePDF 从 PDF 文件中提取文本
// 注意：这是一个简化版本，生产环境应该使用专业的 PDF 解析库
func parsePDF(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	// 简单的文本提取 - 查找文本流
	// 这是一个非常基础的实现，实际项目中应该使用 pdfplumber 或类似库
	content := string(data)

	// 尝试提取可读文本（跳过二进制内容）
	var sb strings.Builder
	inText := false
	for i := 0; i < len(content)-4; i++ {
		// 查找文本流标记
		if content[i] == 'T' && content[i+1] == 'j' && content[i+2] == ' ' {
			inText = true
			continue
		}
		if inText && content[i] == '(' && content[i+1] != ')' {
			// 提取括号内的文本
			j := i + 1
			for j < len(content) && content[j] != ')' {
				if content[j] != '\\' {
					sb.WriteByte(content[j])
				}
				j++
			}
			inText = false
			i = j
		}
	}

	result := sb.String()
	if len(result) < 10 {
		// 如果提取的文本太少，返回提示信息
		return fmt.Sprintf("[PDF 文件 %s - 需要专业 PDF 解析库才能提取完整文本]", filePath), nil
	}

	return result, nil
}

// ============================================================
// Excel 文档解析
// ============================================================

// parseExcel 从 Excel 文件中提取文本
func parseExcel(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	// 简单的文本提取 - 查找可读文本内容
	content := string(data)
	var sb strings.Builder

	// 提取文本内容（跳过二进制数据）
	for i := 0; i < len(content)-1; i++ {
		// 跳过控制字符，保留可读文本
		if content[i] >= 32 && content[i] <= 126 {
			sb.WriteByte(content[i])
		} else if content[i] == '\n' || content[i] == '\r' {
			sb.WriteByte('\n')
		}
	}

	result := sb.String()
	if len(result) < 10 {
		return fmt.Sprintf("[Excel 文件 %s - 需要专业 Excel 解析库才能提取完整文本]", filePath), nil
	}

	return result, nil
}

// ============================================================
// PowerPoint 文档解析
// ============================================================

// parsePowerPoint 从 PowerPoint 文件中提取文本
func parsePowerPoint(filePath string) (string, error) {
	// .pptx 本质是 zip 文件
	zipReader, err := zip.OpenReader(filePath)
	if err != nil {
		return "", err
	}
	defer zipReader.Close()

	var sb strings.Builder

	// 遍历 zip 中的文件
	for _, file := range zipReader.File {
		// 查找幻灯片文件
		if strings.HasPrefix(file.Name, "ppt/slides/slide") && strings.HasSuffix(file.Name, ".xml") {
			rc, err := file.Open()
			if err != nil {
				continue
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				continue
			}

			// 简单的文本提取 - 查找 <a:t> 标签内的文本
			content := string(data)
			inText := false
			for i := 0; i < len(content)-3; i++ {
				if content[i:i+4] == "<a:t>" {
					inText = true
					i += 4
					continue
				}
				if content[i:i+6] == "</a:t>" {
					inText = false
					continue
				}
				if inText && content[i] != '<' {
					sb.WriteByte(content[i])
				}
			}
			sb.WriteString("\n")
		}
	}

	result := sb.String()
	if len(result) < 10 {
		return fmt.Sprintf("[PowerPoint 文件 %s - 需要专业 PPT 解析库才能提取完整文本]", filePath), nil
	}

	return result, nil
}
