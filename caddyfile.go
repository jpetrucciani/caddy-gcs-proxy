package caddygcsproxy

import (
	"strconv"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func init() {
	httpcaddyfile.RegisterHandlerDirective("gcsproxy", parseCaddyfile)
}

// parseCaddyfile parses the gcsproxy directive. It enables the proxying
// requests to GCS and configures it with this syntax:
//
//	gcsproxy [<matcher>] {
//	    root   <path to prefix GCS key with>
//	    bucket <gcs bucket name>
//	    index  <files...>
//	    hide   <file patterns...>
//	    credentials_file <path to credentials file>
//	    project_id <gcp project id>
//	    enable_put
//	    enable_delete
//	    errors [<http code>] [<gcs key to error page>|pass_through]
//	    browse [<template file>]
//	}
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	return parseCaddyfileWithDispenser(h.Dispenser)
}

func parseCaddyfileWithDispenser(h *caddyfile.Dispenser) (*GcsProxy, error) {
	var b GcsProxy

	replacer := caddy.NewReplacer()

	h.NextArg() // skip block beginning: "gcsproxy"
parseLoop:
	for h.NextBlock(0) {
		switch h.Val() {
		case "credentials_file":
			if !h.AllArgs(&b.CredentialsFile) {
				return nil, h.ArgErr()
			}
			b.CredentialsFile = replacer.ReplaceAll(b.CredentialsFile, "")
		case "project_id":
			if !h.AllArgs(&b.ProjectID) {
				return nil, h.ArgErr()
			}
			b.ProjectID = replacer.ReplaceAll(b.ProjectID, "")
		case "root":
			if !h.AllArgs(&b.Root) {
				return nil, h.ArgErr()
			}
		case "hide":
			b.Hide = h.RemainingArgs()
			if len(b.Hide) == 0 {
				return nil, h.ArgErr()
			}
		case "bucket":
			if !h.AllArgs(&b.Bucket) {
				return nil, h.ArgErr()
			}
			b.Bucket = replacer.ReplaceAll(b.Bucket, "")
			if b.Bucket == "" {
				break parseLoop
			}
		case "index":
			b.IndexNames = h.RemainingArgs()
			if len(b.IndexNames) == 0 {
				return nil, h.ArgErr()
			}
		case "enable_put":
			b.EnablePut = true
		case "enable_delete":
			b.EnableDelete = true
		case "browse":
			b.EnableBrowse = true
			args := h.RemainingArgs()
			if len(args) == 1 {
				b.BrowseTemplate = args[0]
			}
			if len(args) > 1 {
				return nil, h.ArgErr()
			}
		case "error_page", "errors":
			if b.ErrorPages == nil {
				b.ErrorPages = make(map[int]string)
			}

			args := h.RemainingArgs()
			if len(args) == 1 {
				b.DefaultErrorPage = args[0]
			} else if len(args) == 2 {
				httpStatusStr := args[0]
				keyOrPassThrough := args[1]

				httpStatus, err := strconv.Atoi(httpStatusStr)
				if err != nil {
					return nil, h.Errf("'%s' is not a valid HTTP status code", httpStatusStr)
				}

				b.ErrorPages[httpStatus] = keyOrPassThrough
			} else {
				return nil, h.ArgErr()
			}
		default:
			return nil, h.Errf("%s not a valid gcsproxy option", h.Val())
		}
	}
	if b.Bucket == "" {
		return nil, h.Err("bucket must be set and not empty")
	}

	return &b, nil
}
