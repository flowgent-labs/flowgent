package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)


// PDFParser PDF 解析器
type PDFParser struct{}

// NewPDFParser 创建 PDF 解析器
func NewPDFParser() *PDFParser {
	return &PDFParser{}
}

// ExtractVulnerabilities 从 PDF 提取漏洞元数据
func (p *PDFParser) ExtractVulnerabilities(ctx context.Context, pdfPath string) ([]Vulnerability, error) {
	// 读取 PDF 内容 (简化实现，实际应使用 pdfplumber 或类似库)
	content, err := p.readPDF(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read PDF: %w", err)
	}

	// 解析漏洞表格
	// 示例格式：| CRITICAL | SQL Injection | /path/file.java | 123 | CVE-2024-1234 | CWE-89 |
	pattern := regexp.MustCompile(`(?m)(CRITICAL|HIGH|MEDIUM|LOW)\s+\|?\s*([|.\w\s]+?)\s*\|?\s*(\/[\w./]+)\s+\|?\s*(\d+)\s*\|?\s*(CVE-\d+-\d+)?\s*\|?\s*(CWE-\d+)?`)

	var vulns []Vulnerability
	matches := pattern.FindAllStringSubmatch(content, -1)

	for _, m := range matches {
		vuln := Vulnerability{
			Severity: strings.TrimSpace(m[1]),
			Type:     strings.TrimSpace(m[2]),
			File:     strings.TrimSpace(m[3]),
			Line:     parseInt(m[4]),
			CVE:      strings.TrimSpace(m[5]),
			CWE:      strings.TrimSpace(m[6]),
		}
		vulns = append(vulns, vuln)
	}

	return vulns, nil
}

// ParseToHTML 解析 PDF 为 HTML (用于调试)
func (p *PDFParser) ParseToHTML(ctx context.Context, pdfPath string) (string, error) {
	// 使用 pdfcpu 或类似库解析
	// 简化实现：返回 HTML 文件路径
	htmlPath := pdfPath + ".html"
	return htmlPath, nil
}

func (p *PDFParser) readPDF(pdfPath string) (string, error) {
	// 简化实现：实际应使用 PDF 解析库
	data, err := os.ReadFile(pdfPath)
	return string(data), err
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// VulnerabilityReport 漏洞报告
type VulnerabilityReport struct {
	ScanType      ScanType        `json:"scan_type"`
	ScanID        string          `json:"scan_id"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities"`
}

// MarshalReport 将报告序列化为 JSON
func MarshalReport(report *VulnerabilityReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
