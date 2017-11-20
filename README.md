
[![GoDoc](https://godoc.org/github.com/myENA/restclient?status.svg)](https://godoc.org/github.com/myENA/restclient)
[![Mozilla Public License](https://img.shields.io/badge/license-MPL-blue.svg)](https://www.mozilla.org/MPL)
[![Build Status](https://travis-ci.org/myENA/restclient.svg?branch=master)](https://travis-ci.org/myENA/restclient)
[![Go Report Card](https://goreportcard.com/badge/github.com/myENA/restclient)](https://goreportcard.com/report/github.com/myENA/restclient)

This is a simple http client package with validation.  It makes use of github.com/go-playground/validator as well as the github.com/google/go-querystring packages.

Package restclient

This is yet another rest http client package for consuming APIs. The goal of this is to cover the most common use cases and be easy to use. This package has two main dependencies,

    gopkg.in/go-playground/validator.v9
and

    github.com/google/go-querystring
There are two main ways to use this, the Client methods, or BaseClient methods. BaseClient is a thin wrapper around Client, but but has a baseURL stored inside the struct so that will be used for each request. This is useful when the client will be hitting endpoints on a single base URL. The other way is with Client, which allows you to specify a baseURL with each call.

Following is an example program that demonstrates how to use this package.

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	rc "github.com/myENA/restclient"
)

type UserCapsRequest struct {
	UID string `url:"uid" validate:"required"`

	// UserCaps - this deonstrates using the validate:"dive" tag to
	// visit members of the list, as well as the url "semicolon" modifier
	// which gives us how to represent members of the slice on the
	// uri string.
	UserCaps []UserCap `url:"user-caps,semicolon" validate:"required,dive"`
}

type UserCap struct {
	// This demonstrates the validate alternation syntax for "enum" type
	// values
	Type string `json:"type" validate:"required,eq=users|eq=buckets|eq=metadata|eq=usage|eq=zone"`

	// This demonstrates how to represent an embedded comma character in
	// alternation syntax.
	Permission string `json:"perm" validate:"required,eq=*|eq=read|eq=write|eq=read0x2Cwrite"`
}

// String - Implement Stringer, informs go query string how to represent
// this struct.
func (uc UserCap) String() string {
	return uc.Type + "=" + uc.Permission
}

func main() {
	u, _ := url.Parse("http://localhost:8200/admin")
	bc := &rc.BaseClient{
		Client: &rc.Client{
			Client: &http.Client{},
			FixupCallback: func(req *http.Request) error {
				req.Header.Set("User-Agent", "TotallyChromeAndStuff")
				return nil
			},
		},
		BaseURL: u,
	}
	ucr := &UserCapsRequest{
		UID: "59",
		UserCaps: []UserCap{
			UserCap{
				Type:       "users",
				Permission: "*",
			},
			UserCap{
				Type:       "buckets",
				Permission: "*",
			},
			UserCap{
				Type:       "metadata",
				Permission: "*",
			},
			UserCap{
				Type:       "usage",
				Permission: "*",
			},
			UserCap{
				Type:       "zone",
				Permission: "*",
			},
		},
	}
	resp := []UserCap{}
	err := bc.Put(context.Background(), "/user?caps", ucr, nil, &resp)
	if err != nil {
		if _, ok := err.(rc.ValidationErrors); ok {
			// We had an error on validation from the query string.
			// Note that there is no pointer on the type assertion.  This
			// follows the playground validator package ValidationErrors.

			fmt.Println("Got a validation error: ", err)
		} else if e, ok := err.(*rc.ResponseError); ok {
			// We had a response code >= 400 from the server.  Note
			// the pointer on the type assertion above.  This
			// is to avoid breaking backwards compatibility.

			fmt.Printf("Got a response error code: %d\n", e.StatusCode)
		} else {
			// Something else went wrong.
			fmt.Println("Got unknown error: ", err)
		}
	}
}
```
