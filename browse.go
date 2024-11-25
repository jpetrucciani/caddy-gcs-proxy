package caddygcsproxy

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/dustin/go-humanize"
	"google.golang.org/api/iterator"
)

var bufPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

type PageObj struct {
	Count    int64  `json:"count"`
	Items    []Item `json:"items"`
	MoreLink string `json:"more"`
}

type Item struct {
	Name         string `json:"name"`
	IsDir        bool   `json:"is_dir"`
	Key          string `json:"key"`
	Url          string `json:"url"`
	Size         string `json:"size"`
	LastModified string `json:"last_modified"`
}

func (po PageObj) GenerateJson(w http.ResponseWriter) error {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	err := json.NewEncoder(buf).Encode(po)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, err = buf.WriteTo(w)
	return err
}

func (p GcsProxy) ConstructListParams(r *http.Request, key string) *storage.Query {
	prefix := strings.TrimPrefix(key, "/")

	query := &storage.Query{
		Prefix:    prefix,
		Delimiter: "/",
	}

	maxPerPage := r.URL.Query().Get("max")
	if maxPerPage != "" {
		// maxKeys, err := strconv.ParseInt(maxPerPage, 10, 64)
		// if err == nil && maxKeys > 0 && maxKeys <= 1000 {
		// 	query.MaxResults = int(maxKeys)
		// }
	}

	if pageToken := r.URL.Query().Get("next"); pageToken != "" {
		query.StartOffset = pageToken
	}

	return query
}

func (po PageObj) GenerateHtml(w http.ResponseWriter, template *template.Template) error {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	err := template.Execute(buf, po)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = buf.WriteTo(w)
	return err
}

func (p GcsProxy) MakePageObj(it *storage.ObjectIterator) (PageObj, error) {
	po := PageObj{}

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return PageObj{}, err
		}

		// Increment count for each item
		po.Count++

		if attrs.Prefix != "" {
			// This is a directory
			name := path.Base(attrs.Prefix)
			dirPath := "./" + name + "/"
			po.Items = append(po.Items, Item{
				Url:   dirPath,
				Name:  name,
				IsDir: true,
			})
		} else {
			// This is a file
			name := path.Base(attrs.Name)
			itemPath := "./" + name
			size := humanize.Bytes(uint64(attrs.Size))
			timeAgo := humanize.Time(attrs.Updated)
			po.Items = append(po.Items, Item{
				Name:         name,
				Key:          attrs.Name,
				Url:          itemPath,
				Size:         size,
				LastModified: timeAgo,
				IsDir:        false,
			})
		}
	}

	// If there's a next page token, create the MoreLink
	if token := it.PageInfo().Token; token != "" {
		var nextUrl url.URL
		queryItems := nextUrl.Query()
		queryItems.Add("next", token)
		if it.PageInfo().MaxSize > 0 {
			queryItems.Add("max", strconv.FormatInt(int64(it.PageInfo().MaxSize), 10))
		}
		nextUrl.RawQuery = queryItems.Encode()
		po.MoreLink = nextUrl.String()
	}

	return po, nil
}

// This is a lame ass default template - needs to get better
const defaultBrowseTemplate = `<!DOCTYPE html>
<html>
        <body>
                <ul>
                {{- range .PageObj }}
                <li>
                {{- if .IsDir}}
                <a href="{{html .Url}}">{{html .Name}}</a>
                {{- else}}
                <a href="{{html .Url}}">{{html .Name}}</a> Size: {{html .Size}} Last Modified: {{html .LastModified}}
                {{- end}}
                </li>
                {{- end }}
                </ul>
		<p>number of items: {{ .Count }}</p>
		{{- if .MoreLink }}
		<a href="{{ html .MoreLink }}">more...</a>
		{{- end }}
        </body>
</html>`
