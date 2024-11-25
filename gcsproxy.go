package caddygcsproxy

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"context"

	"cloud.google.com/go/storage"
	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

var defaultIndexNames = []string{"index.html", "index.txt"}

func init() {
	caddy.RegisterModule(GcsProxy{})
}

// GcsProxy implements a proxy to return, set, delete or browse objects from GCS
type GcsProxy struct {
	// The path to the root of the site. Default is `{http.vars.root}` if set,
	// Or if not set the value is "" - meaning use the whole path as a key.
	Root string `json:"root,omitempty"`

	// The name of the GCS bucket
	Bucket string `json:"bucket,omitempty"`

	// The names of files to try as index files if a folder is requested.
	IndexNames []string `json:"index_names,omitempty"`

	// A glob pattern used to hide matching key paths (returning a 404)
	Hide []string

	// Flag to determine if PUT operations are allowed (default false)
	EnablePut bool

	// Flag to determine if DELETE operations are allowed (default false)
	EnableDelete bool

	// Flag to enable browsing of "directories" in GCS (paths that end with a /)
	EnableBrowse bool

	// Path to a template file to use for generating browse dir html page
	BrowseTemplate string

	// Mapping of HTTP error status to GCS keys or pass through option.
	ErrorPages map[int]string `json:"error_pages,omitempty"`

	// GCS key to a default error page or pass through option.
	DefaultErrorPage string `json:"default_error_page,omitempty"`

	// Add GCS-specific fields
	ProjectID       string `json:"project_id,omitempty"`
	CredentialsFile string `json:"credentials_file,omitempty"`

	client      *storage.Client
	bucket      *storage.BucketHandle
	dirTemplate *template.Template
	log         *zap.Logger
}

// CaddyModule returns the Caddy module information.
func (GcsProxy) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.gcsproxy",
		New: func() caddy.Module { return new(GcsProxy) },
	}
}

func (p *GcsProxy) Provision(ctx caddy.Context) (err error) {
	p.log = ctx.Logger(p)

	if p.Root == "" {
		p.Root = "{http.vars.root}"
	}

	if p.IndexNames == nil {
		p.IndexNames = defaultIndexNames
	}

	if p.ErrorPages == nil {
		p.ErrorPages = make(map[int]string)
	}

	if p.EnableBrowse {
		var tpl *template.Template
		var err error

		if p.BrowseTemplate != "" {
			tpl, err = template.ParseFiles(p.BrowseTemplate)
			if err != nil {
				return fmt.Errorf("parsing browse template file: %v", err)
			}
		} else {
			tpl, err = template.New("default_listing").Parse(defaultBrowseTemplate)
			if err != nil {
				return fmt.Errorf("parsing default browse template: %v", err)
			}
		}
		p.dirTemplate = tpl
	}

	// Create GCS client
	var opts []option.ClientOption
	if p.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(p.CredentialsFile))
	}

	client, err := storage.NewClient(context.Background(), opts...)
	if err != nil {
		p.log.Error("could not create GCS client",
			zap.String("error", err.Error()),
		)
		return err
	}

	p.client = client
	p.bucket = client.Bucket(p.Bucket)
	p.log.Info("GCS proxy initialized for bucket: " + p.Bucket)

	return nil
}

func (p GcsProxy) getGcsObject(bucket string, path string, headers http.Header) (*storage.Reader, error) {
	ctx := context.Background()
	obj := p.bucket.Object(path)
	if ifMatch := headers.Get("If-Match"); ifMatch != "" {
		// Parse generation from ETag which is in format "\"<generation>\""
		if len(ifMatch) > 2 {
			gen, err := strconv.ParseInt(ifMatch[1:len(ifMatch)-1], 10, 64)
			if err == nil {
				obj = obj.If(storage.Conditions{GenerationMatch: gen})
			}
		}
	}
	// ... handle other conditions ...

	return obj.NewReader(ctx)
}

func joinPath(root string, uriPath string) string {
	isDir := uriPath[len(uriPath)-1:] == "/"
	newPath := path.Join(root, uriPath)
	if isDir && newPath != "/" {
		// Join will strip the ending /
		// add it back if it was there as it implies a dir view
		return newPath + "/"
	}
	return newPath
}

func (p GcsProxy) PutHandler(w http.ResponseWriter, r *http.Request, key string) error {
	ctx := context.Background()
	obj := p.bucket.Object(key)
	writer := obj.NewWriter(ctx)

	// Copy headers
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		writer.ContentType = contentType
	}
	// ... copy other relevant headers ...

	if _, err := io.Copy(writer, r.Body); err != nil {
		return convertToCaddyError(err)
	}
	if err := writer.Close(); err != nil {
		return convertToCaddyError(err)
	}

	// Set ETag header from object generation
	attrs, err := obj.Attrs(ctx)
	if err == nil {
		w.Header().Set("ETag", fmt.Sprintf("\"%d\"", attrs.Generation))
	}

	return nil
}

func (p GcsProxy) DeleteHandler(w http.ResponseWriter, r *http.Request, key string) error {
	isDir := strings.HasSuffix(key, "/")
	if isDir || !p.EnableDelete {
		err := errors.New("method not allowed")
		return caddyhttp.Error(http.StatusMethodNotAllowed, err)
	}
	ctx := context.Background()
	obj := p.bucket.Object(key)
	err := obj.Delete(ctx)
	if err != nil {
		return convertToCaddyError(err)
	}

	return nil
}

func (p GcsProxy) BrowseHandler(w http.ResponseWriter, r *http.Request, key string) error {
	ctx := context.Background()

	// Create a prefix iterator
	it := p.bucket.Objects(ctx, &storage.Query{
		Prefix:    key,
		Delimiter: "/",
	})

	var result []storage.ObjectAttrs
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return convertToCaddyError(err)
		}
		result = append(result, *attrs)
	}

	// Convert result to your page object format and generate response
	// ... existing response generation code ...

	return nil
}

func (p GcsProxy) writeResponseFromGetObject(w http.ResponseWriter, reader *storage.Reader, attrs *storage.ObjectAttrs) error {
	// Copy headers from GCS response to our response
	if attrs.CacheControl != "" {
		w.Header().Set("Cache-Control", attrs.CacheControl)
	}
	if attrs.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", attrs.ContentDisposition)
	}
	if attrs.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", attrs.ContentEncoding)
	}
	if attrs.ContentLanguage != "" {
		w.Header().Set("Content-Language", attrs.ContentLanguage)
	}
	if attrs.ContentType != "" {
		w.Header().Set("Content-Type", attrs.ContentType)
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", attrs.Generation))
	if !attrs.Updated.IsZero() {
		w.Header().Set("Last-Modified", attrs.Updated.UTC().Format(http.TimeFormat))
	}

	// Copy metadata
	for key, value := range attrs.Metadata {
		w.Header().Set(key, value)
	}

	// Copy the body
	if reader != nil {
		_, err := io.Copy(w, reader)
		return err
	}

	return nil
}

func (p GcsProxy) serveErrorPage(w http.ResponseWriter, gcsKey string) error {
	ctx := context.Background()
	obj := p.bucket.Object(gcsKey)

	reader, err := obj.NewReader(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()

	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return err
	}

	return p.writeResponseFromGetObject(w, reader, attrs)
}

// ServeHTTP implements the main entry point for a request for the caddyhttp.Handler interface.
func (p GcsProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)

	fullPath := joinPath(repl.ReplaceAll(p.Root, ""), r.URL.Path)

	var err error
	switch r.Method {
	case http.MethodGet:
		err = p.GetHandler(w, r, fullPath)
	case http.MethodPut:
		err = p.PutHandler(w, r, fullPath)
	case http.MethodDelete:
		err = p.DeleteHandler(w, r, fullPath)
	default:
		err = caddyhttp.Error(http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
	if err == nil {
		// Success!
		return nil
	}

	// Make the err a caddyErr if it is not already
	caddyErr, isCaddyErr := err.(caddyhttp.HandlerError)
	if !isCaddyErr {
		caddyErr = caddyhttp.Error(http.StatusInternalServerError, err)
	}

	// If non OK status code - WriteHeader - except for GET method, where we still need to process more
	if r.Method != http.MethodGet {
		if caddyErr.StatusCode != 0 {
			w.WriteHeader(caddyErr.StatusCode)
		}
		return caddyErr
	}

	// Certain errors we will not pass through
	if caddyErr.StatusCode == http.StatusNotModified ||
		caddyErr.StatusCode == http.StatusPreconditionFailed ||
		caddyErr.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		w.WriteHeader(caddyErr.StatusCode)
		return caddyErr
	}

	// process errors directive
	doPassThrough, doGCSErrorPage, key := p.determineErrorsAction(caddyErr.StatusCode)
	if doPassThrough {
		return next.ServeHTTP(w, r)
	}

	if caddyErr.StatusCode != 0 {
		w.WriteHeader(caddyErr.StatusCode)
	}
	if doGCSErrorPage {
		if err := p.serveErrorPage(w, key); err != nil {
			// Just log the error as we don't want to swallow the parent error.
			p.log.Error("error serving error page",
				zap.String("bucket", p.Bucket),
				zap.String("key", key),
				zap.String("err", err.Error()),
			)
		}
	}
	return caddyErr
}

func (p GcsProxy) determineErrorsAction(statusCode int) (bool, bool, string) {
	var key string
	if errorPageGCSKey, hasErrorPageForCode := p.ErrorPages[statusCode]; hasErrorPageForCode {
		key = errorPageGCSKey
	} else if p.DefaultErrorPage != "" {
		key = p.DefaultErrorPage
	}

	if strings.ToLower(key) == "pass_through" {
		return true, false, ""
	}

	return false, key != "", key
}

func (p GcsProxy) GetHandler(w http.ResponseWriter, r *http.Request, fullPath string) error {
	if fileHidden(fullPath, p.Hide) {
		return caddyhttp.Error(http.StatusNotFound, nil)
	}

	isDir := strings.HasSuffix(fullPath, "/")
	var reader *storage.Reader
	var attrs *storage.ObjectAttrs
	var err error
	ctx := context.Background()

	if isDir && len(p.IndexNames) > 0 {
		for _, indexPage := range p.IndexNames {
			indexPath := path.Join(fullPath, indexPage)
			obj := p.bucket.Object(indexPath)

			reader, err = obj.NewReader(ctx)
			if err == nil {
				attrs, err = obj.Attrs(ctx)
				if err == nil {
					isDir = false
					break
				}
				reader.Close()
			}

			if err != nil {
				if err == storage.ErrObjectNotExist {
					continue
				}
				p.log.Warn("error when looking for index",
					zap.String("bucket", p.Bucket),
					zap.String("key", indexPath),
					zap.String("err", err.Error()),
				)
			}
		}
	}

	if isDir {
		if p.EnableBrowse {
			return p.BrowseHandler(w, r, fullPath)
		} else {
			err = errors.New("cannot view a directory")
			return caddyhttp.Error(http.StatusForbidden, err)
		}
	}

	if reader == nil {
		obj := p.bucket.Object(fullPath)
		reader, err = obj.NewReader(ctx)
		if err != nil {
			if err == storage.ErrObjectNotExist {
				p.log.Debug("not found",
					zap.String("bucket", p.Bucket),
					zap.String("key", fullPath),
				)
				return caddyhttp.Error(http.StatusNotFound, err)
			}
			p.log.Error("failed to get object",
				zap.String("bucket", p.Bucket),
				zap.String("key", fullPath),
				zap.String("err", err.Error()),
			)
			return convertToCaddyError(err)
		}
		defer reader.Close()

		attrs, err = obj.Attrs(ctx)
		if err != nil {
			return convertToCaddyError(err)
		}
	}

	return p.writeResponseFromGetObject(w, reader, attrs)
}

// fileHidden returns true if filename is hidden
// according to the hide list.
func fileHidden(filename string, hide []string) bool {
	sep := string(filepath.Separator)
	var components []string

	for _, h := range hide {
		if !strings.Contains(h, sep) {
			// if there is no separator in h, then we assume the user
			// wants to hide any files or folders that match that
			// name; thus we have to compare against each component
			// of the filename, e.g. hiding "bar" would hide "/bar"
			// as well as "/foo/bar/baz" but not "/barstool".
			if len(components) == 0 {
				components = strings.Split(filename, sep)
			}
			for _, c := range components {
				if c == h {
					return true
				}
			}
		} else if strings.HasPrefix(filename, h) {
			// otherwise, if there is a separator in h, and
			// filename is exactly prefixed with h, then we
			// can do a prefix match so that "/foo" matches
			// "/foo/bar" but not "/foobar".
			withoutPrefix := strings.TrimPrefix(filename, h)
			if strings.HasPrefix(withoutPrefix, sep) {
				return true
			}
		}

		// in the general case, a glob match will suffice
		if hidden, _ := filepath.Match(h, filename); hidden {
			return true
		}
	}

	return false
}

func convertToCaddyError(err error) error {
	if err == nil {
		return nil
	}

	if err == storage.ErrObjectNotExist {
		return caddyhttp.Error(http.StatusNotFound, err)
	}

	// Add more specific error conversions as needed
	return caddyhttp.Error(http.StatusInternalServerError, err)
}
