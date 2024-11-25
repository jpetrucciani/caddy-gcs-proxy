package caddygcsproxy

import (
	"reflect"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

type testCase struct {
	desc      string
	input     string
	shouldErr bool
	errString string
	obj       GcsProxy
}

func TestParseCaddyfile(t *testing.T) {
	testCases := []testCase{
		{
			desc: "bad sub directive",
			input: `gcsproxy {
				foo
			}`,
			shouldErr: true,
			errString: "Testfile:2 - Error during parsing: foo not a valid gcsproxy option",
		},
		{
			desc: "bucket bad # args",
			input: `gcsproxy {
			bucket
			}`,
			shouldErr: true,
			errString: "Testfile:2 - Error during parsing: Wrong argument count or unexpected line ending after 'bucket'",
		},
		{
			desc: "bucket empty string",
			input: `gcsproxy {
				bucket ""
			}`,
			shouldErr: true,
			errString: "Testfile:2 - Error during parsing: bucket must be set and not empty",
		},
		{
			desc: "bucket missing",
			input: `gcsproxy {
				region foo
			}`,
			shouldErr: true,
			errString: "Testfile:3 - Error during parsing: bucket must be set and not empty",
		},
		{
			desc: "endpoint bad # args",
			input: `gcsproxy {
				endpoint
			}`,
			shouldErr: true,
			errString: "Testfile:2 - Error during parsing: Wrong argument count or unexpected line ending after 'endpoint'",
		},
		{
			desc: "region bad # args",
			input: `gcsproxy {
				region one two
			}`,
			shouldErr: true,
			errString: "Testfile:2 - Error during parsing: Wrong argument count or unexpected line ending after 'one'",
		},
		{
			desc: "root bad # args",
			input: `gcsproxy {
				root one two
			}`,
			shouldErr: true,
			errString: "Testfile:2 - Error during parsing: Wrong argument count or unexpected line ending after 'one'",
		},
		{
			desc: "errors on invalid HTTP status for errors",
			input: `gcsproxy {
				bucket mybucket
				errors invalid "path/to/404.html"
			}`,
			shouldErr: true,
			errString: "Testfile:3 - Error during parsing: 'invalid' is not a valid HTTP status code",
		},
		{
			desc: "errors on too many arguments for errors",
			input: `gcsproxy {
				bucket mybucket
				errors 403 "path/to/404.html" "what's this?"
			}`,
			shouldErr: true,
			errString: "Testfile:3 - Error during parsing: Wrong argument count or unexpected line ending after 'what's this?'",
		},
		{
			desc: "enable pu",
			input: `gcsproxy {
				bucket mybucket
				enable_put
			}`,
			shouldErr: false,
			obj: GcsProxy{
				Bucket:    "mybucket",
				EnablePut: true,
			},
		},
		{
			desc: "enable delete",
			input: `gcsproxy {
				bucket mybucket
				enable_delete
			}`,
			shouldErr: false,
			obj: GcsProxy{
				Bucket:       "mybucket",
				EnableDelete: true,
			},
		},
		{
			desc: "enable error pages",
			input: `gcsproxy {
				bucket mybucket
				errors 404 "path/to/404.html"
				errors 403 "path/to/403.html"
				errors "path/to/default_error.html"
			}`,
			shouldErr: false,
			obj: GcsProxy{
				Bucket: "mybucket",
				ErrorPages: map[int]string{
					403: "path/to/403.html",
					404: "path/to/404.html",
				},
				DefaultErrorPage: "path/to/default_error.html",
			},
		},
		{
			desc: "hide files",
			input: `gcsproxy {
				bucket mybucket
				hide foo.txt _*
			}`,
			shouldErr: false,
			obj: GcsProxy{
				Bucket: "mybucket",
				Hide:   []string{"foo.txt", "_*"},
			},
		},
		{
			desc: "hide files - missing arg",
			input: `gcsproxy {
				bucket mybucket
				hide
			}`,
			shouldErr: true,
			errString: "Testfile:3 - Error during parsing: Wrong argument count or unexpected line ending after 'hide'",
		},
		{
			desc: "index test",
			input: `gcsproxy {
				bucket mybucket
				index i.htm i.html
			}`,
			shouldErr: false,
			obj: GcsProxy{
				Bucket:     "mybucket",
				IndexNames: []string{"i.htm", "i.html"},
			},
		},
		{
			desc: "index - missing arg",
			input: `gcsproxy {
				bucket mybucket
				index
			}`,
			shouldErr: true,
			errString: "Testfile:3 - Error during parsing: Wrong argument count or unexpected line ending after 'index'",
		},
	}

	for _, tc := range testCases {
		d := caddyfile.NewTestDispenser(tc.input)
		prox, err := parseCaddyfileWithDispenser(d)

		if tc.shouldErr {
			if err == nil {
				t.Errorf("Test case '%s' expected an err and did not get one", tc.desc)
			}
			if prox != nil {
				t.Errorf("Test case '%s' expected an nil obj but it was not nil", tc.desc)
			}
			if err.Error() != tc.errString {
				t.Errorf("Test case '%s' expected err '%s' but got '%s'", tc.desc, tc.errString, err.Error())
			}
		} else {
			if err != nil {
				t.Errorf("Test case '%s' unexpected err '%s'", tc.desc, err.Error())
			}
			if prox == nil {
				t.Errorf("Test case '%s' return object was nil", tc.desc)
				continue
			}
			if !reflect.DeepEqual(*prox, tc.obj) {
				t.Errorf("Test case '%s' expected Endpoint of  '%#v' but got '%#v'", tc.desc, tc.obj, prox)
			}
		}
	}
}
