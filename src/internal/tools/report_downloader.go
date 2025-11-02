package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ReportDownloader PDF 报告下载器
type ReportDownloader struct {
	httpClient *http.Client
	outputDir  string
	apiToken   string
}

// NewReportDownloader 创建报告下载器
func NewReportDownloader(outputDir, apiToken string) *ReportDownloader {
	os.MkdirAll(outputDir, 0755)
	return &ReportDownloader{
		httpClient: &http.Client{Timeout: 5 * time.Minute},
		outputDir:  outputDir,
		apiToken:   apiToken,
	}
}

// Download 下载单个扫描报告
func (d *ReportDownloader) Download(ctx context.Context, job ScanJob, reportURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", reportURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	if d.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiToken)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// 保存文件
	filename := fmt.Sprintf("%s_%s_report.pdf", job.Type, job.JobID[:8])
	filepath := filepath.Join(d.outputDir, filename)

	outFile, err := os.Create(filepath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return "", fmt.Errorf("copy content: %w", err)
	}

	return filepath, nil
}

// DownloadAll 批量下载所有报告
func (d *ReportDownloader) DownloadAll(ctx context.Context, jobs []ScanJob, reportURLs map[string]string) (map[ScanType]string, error) {
	paths := make(map[ScanType]string)
	for _, job := range jobs {
		url, ok := reportURLs[job.JobID]
		if !ok {
			return nil, fmt.Errorf("no URL for job %s", job.JobID)
		}
		path, err := d.Download(ctx, job, url)
		if err != nil {
			return nil, fmt.Errorf("download %s failed: %w", job.Type, err)
		}
		paths[job.Type] = path
	}
	return paths, nil
}
