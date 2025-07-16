package anna

import (
	"fmt"
	"net/url"

	"strings"

	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	colly "github.com/gocolly/colly/v2"
	"github.com/iosifache/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const (
	AnnasSearchEndpoint   = "https://annas-archive.org/search?q=%s"
	AnnasDownloadEndpoint = "https://annas-archive.org/dyn/api/fast_download.json?md5=%s&key=%s"
)

func extractMetaInformation(meta string) (language, format, size string) {
	parts := strings.Split(meta, ", ")
	if len(parts) < 4 {
		return "", "", ""
	}

	language = parts[0]
	format = parts[1]
	size = parts[3]

	return language, format, size
}

func FindBook(query string) ([]*Book, error) {
	l := logger.GetLogger()

	c := colly.NewCollector(
		colly.Async(true),
	)

	bookList := make([]*colly.HTMLElement, 0)

	c.OnHTML("a", func(e *colly.HTMLElement) {
		if strings.Index(e.Attr("href"), "/md5/") == 0 {
			bookList = append(bookList, e)
		}
	})

	c.OnRequest(func(r *colly.Request) {
		l.Info("Visiting URL", zap.String("url", r.URL.String()))
	})

	fullURL := fmt.Sprintf(AnnasSearchEndpoint, url.QueryEscape(query))
	c.Visit(fullURL)
	c.Wait()

	bookListParsed := make([]*Book, 0)
	for _, e := range bookList {
		meta := e.DOM.Parent().Find("div.relative.top-\\[-1\\].pl-4.grow.overflow-hidden > div").Eq(0).Text()
		title := e.DOM.Parent().Find("div.relative.top-\\[-1\\].pl-4.grow.overflow-hidden > h3").Text()
		publisher := e.DOM.Parent().Find("div.relative.top-\\[-1\\].pl-4.grow.overflow-hidden > div").Eq(1).Text()
		authors := e.DOM.Parent().Find("div.relative.top-\\[-1\\].pl-4.grow.overflow-hidden > div").Eq(2).Text()

		language, format, size := extractMetaInformation(meta)

		link := e.Attr("href")
		hash := strings.TrimPrefix(link, "/md5/")

		trimmedFormat := strings.TrimSpace(format)
		if len(trimmedFormat) > 0 {
			trimmedFormat = trimmedFormat[1:]
		}

		book := &Book{
			Language:  strings.TrimSpace(language),
			Format:    trimmedFormat,
			Size:      strings.TrimSpace(size),
			Title:     strings.TrimSpace(title),
			Publisher: strings.TrimSpace(publisher),
			Authors:   strings.TrimSpace(authors),
			URL:       e.Request.AbsoluteURL(link),
			Hash:      hash,
		}

		bookListParsed = append(bookListParsed, book)
	}

	return bookListParsed, nil
}

func (b *Book) Download(secretKey, folderPath string) error {
	apiURL := fmt.Sprintf(AnnasDownloadEndpoint, b.Hash, secretKey)

	resp, err := http.Get(apiURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var apiResp fastDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return err
	}
	if apiResp.DownloadURL == "" {
		if apiResp.Error != "" {
			return errors.New(apiResp.Error)
		}
		return errors.New("failed to get download URL")
	}

	downloadResp, err := http.Get(apiResp.DownloadURL)
	if err != nil {
		return err
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode != http.StatusOK {
		return errors.New("failed to download file")
	}

	filename := b.Title + "." + b.Format
	filename = strings.ReplaceAll(filename, "/", "_")
	filePath := filepath.Join(folderPath, filename)

	out, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, downloadResp.Body)
	return err
}

func (b *Book) String() string {
	return fmt.Sprintf("Title: %s\nAuthors: %s\nPublisher: %s\nLanguage: %s\nFormat: %s\nSize: %s\nURL: %s\nHash: %s",
		b.Title, b.Authors, b.Publisher, b.Language, b.Format, b.Size, b.URL, b.Hash)
}

func (b *Book) ToJSON() (string, error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}
