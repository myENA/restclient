package restclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/spkg/bom"
	"gopkg.in/go-playground/validator.v9"
)

var validate *validator.Validate

var altMatch = regexp.MustCompile(`eq=([^=\|]+)`)

func init() {
	validate = validator.New()
}

// FixupCallback - this is a method that will get called before every request
// so that you can, for instance, manipulate headers for auth purposes, for
// instance.
type FixupCallback func(req *http.Request) error

// ErrorResponseCallback - this allows you to hook into the error response,
// for response codes that are >= 400.  If error returned here is nil,
// processing continues.  Otherwise, the error returned is bubbled to caller.
type ErrorResponseCallback func(resp *http.Response) error

// Client - admin api struct
type Client struct {
	Client             *http.Client
	rawValidatorErrors bool
	// Specifying this allows you to modify headers, add
	// auth tokens or signatures etc before the request is sent.
	FixupCallback FixupCallback

	// ErrorResponseCallback - allows you to specify custom behavior
	// on responses that are >= 400 status code.
	ErrorResponseCallback ErrorResponseCallback

	// StripBOM - setting this to true gives you the option to strip
	// byte order markings from certain responses.
	StripBOM bool

	// FormEncodedBody - setting this to true uses x-www-form-urlencoded.
	// false (default) will do json encoding.
	FormEncodedBody bool

	// SkipValidate - setting this to true bypasses validator run.
	SkipValidate bool
}

// CustomDecoder - If a response struct implements this interface,
// calls the Decode() method instead of json.Unmarshal.
type CustomDecoder interface {
	Decode(data io.Reader) error
}

// NewClient - Client factory method. - if transport is nil, build one
// using config data in cfg.  This is optional, you can also initialize
// the following way:
//
//    cl := &restclient.Client{Client: &http.Client{}}
func NewClient(cfg *ClientConfig, transport http.RoundTripper) (*Client, error) {
	c := &Client{}
	var err error

	if cfg == nil {
		cfg = defConfig()
	}

	if transport == nil {
		// Lifted from http package DefaultTransort.
		t := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		tlsc := new(tls.Config)
		tlsc.InsecureSkipVerify = cfg.InsecureSkipVerify

		var cacerts []byte
		if len(cfg.CACertBundle) > 0 {
			cacerts = cfg.CACertBundle
		} else if cfg.CACertBundlePath != "" {
			cacerts, err = ioutil.ReadFile(cfg.CACertBundlePath)
			if err != nil {
				return nil, fmt.Errorf("Cannot open ca cert bundle %s: %s", cfg.CACertBundlePath, err)
			}
		}

		if len(cacerts) > 0 {
			bundle := x509.NewCertPool()
			ok := bundle.AppendCertsFromPEM(cacerts)
			if !ok {
				return nil, fmt.Errorf("Invalid cert bundle")
			}
			tlsc.RootCAs = bundle
			tlsc.BuildNameToCertificate()
		}

		t.TLSClientConfig = tlsc
		transport = t
	}

	c.Client = &http.Client{
		Timeout:   time.Duration(cfg.ClientTimeout),
		Transport: transport,
	}

	c.FixupCallback = cfg.FixupCallback

	if err != nil {
		return nil, err
	}

	return c, nil
}

func defConfig() *ClientConfig {
	return &ClientConfig{
		ClientTimeout: Duration(3 * time.Second),
	}
}

// Get - makes an http GET request to baseURL with path appended, and queryStruct optionally
// parsed by go-querystring and validated with go-playground/validator.v9.  Upon successful
// request, response is unmarshaled as json into responseBody, unless responseBody implements
// CustomDecoder, in which case Decode() is called.
func (cl *Client) Get(ctx context.Context, baseURL *url.URL, path string, queryStruct interface{}, responseBody interface{}) error {
	_, err := cl.Req(ctx, baseURL, "GET", path, queryStruct, nil, responseBody)
	return err
}

// Delete - makes an http DELETE request to baseURL with path appended, and queryStruct optionally
// parsed by go-querystring and validated with go-playground/validator.v9.  Upon successful
// request, response is unmarshaled as json into responseBody, unless responseBody implements
// CustomDecoder, in which case Decode() is called.
func (cl *Client) Delete(ctx context.Context, baseURL *url.URL, path string, queryStruct interface{}, responseBody interface{}) error {
	_, err := cl.Req(ctx, baseURL, "DELETE", path, queryStruct, nil, responseBody)
	return err
}

// Post - makes an http POST request to baseURL with path appended, and queryStruct optionally
// parsed by go-querystring and validated with go-playground/validator.v9.  requestBody is
// passed to go-playground/validator.v9 and is sent json-encoded as the body.  Upon successful
// request, response is unmarshaled as json into responseBody, unless responseBody implements
// CustomDecoder, in which case Decode() is called.
func (cl *Client) Post(ctx context.Context, baseURL *url.URL, path string, queryStruct, requestBody interface{}, responseBody interface{}) error {
	_, err := cl.Req(ctx, baseURL, "POST", path, queryStruct, requestBody, responseBody)
	return err
}

// Put - makes an http PUT request to baseURL with path appended, and queryStruct optionally
// parsed by go-querystring and validated with go-playground/validator.v9.  requestBody is
// passed to go-playground/validator.v9 and is sent json-encoded as the body.  Upon successful
// request, response is unmarshaled as json into responseBody, unless responseBody implements
// CustomDecoder, in which case Decode() is called.
func (cl *Client) Put(ctx context.Context, baseURL *url.URL, path string, queryStruct, requestBody interface{}, responseBody interface{}) error {
	_, err := cl.Req(ctx, baseURL, "PUT", path, queryStruct, requestBody, responseBody)
	return err
}

func isNil(i interface{}) bool {
	if i == nil {
		return true
	}
	v := reflect.ValueOf(i)

	switch v.Kind() {
	case reflect.Ptr, reflect.Slice:
		return v.IsNil()

	default:
		panic("Invalid interface type: " + v.Kind().String())
	}
}

// Req - like the method-specific versions above, this is the general purpose.
// the *http.Response return value will either be nil or return with the Body
// closed and fully read.  This is mainly useful for inspecting headers, status
// code etc.
func (cl *Client) Req(ctx context.Context, baseURL *url.URL, method, path string,
	queryStruct, requestBody, responseBody interface{}) (*http.Response, error) {
	finurl := baseURL.String()
	if path != "" {
		path = strings.TrimLeft(path, "/")
		finurl = baseURL.String() + "/" + path
	}
	if !isNil(queryStruct) {
		if !cl.SkipValidate {
			err := cl.validate(queryStruct)
			if err != nil {
				return nil, err
			}
		}
		v, err := query.Values(queryStruct)
		if err != nil {
			return nil, err
		}

		qs := v.Encode()

		if qs != "" {
			if strings.Contains(finurl, "?") {
				finurl = finurl + "&" + qs
			} else {
				finurl = finurl + "?" + qs
			}
		}
	}

	var bodyReader io.Reader
	var contentLength int64
	if !isNil(requestBody) {
		if !cl.SkipValidate {

			err := cl.validate(requestBody)
			if err != nil {
				return nil, err
			}
		}
		if cl.FormEncodedBody {
			v, err := query.Values(requestBody)
			if err != nil {
				return nil, err
			}

			rawBody := v.Encode()
			contentLength = int64(len(rawBody))
			bodyReader = strings.NewReader(rawBody)
		} else {
			bjson, err := json.Marshal(requestBody)
			if err != nil {
				return nil, err
			}
			bodyReader = bytes.NewReader(bjson)
			contentLength = int64(len(bjson))
		}
	}
	req, err := http.NewRequest(method, finurl, bodyReader)
	if err != nil {
		return nil, err
	}

	req = req.WithContext(ctx)

	req.ContentLength = contentLength
	if cl.FormEncodedBody {
		req.Header["Content-Type"] = []string{"application/x-www-form-urlencoded"}
	} else {
		req.Header["Content-Type"] = []string{"application/json"}
	}

	if cl.FixupCallback != nil {
		err = cl.FixupCallback(req)
		if err != nil {
			return nil, err
		}
	}
	resp, err := cl.Client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 400 {
		if cl.ErrorResponseCallback != nil {
			err = cl.ErrorResponseCallback(resp)
			if err != nil {
				return resp, err
			}
		} else {
			body, _ := ioutil.ReadAll(resp.Body)
			rs := &ResponseError{
				Status:       resp.Status,
				StatusCode:   resp.StatusCode,
				ResponseBody: body,
				Header:       resp.Header,
			}
			return resp, rs
		}
	}
	if isNil(responseBody) {
		// throw away body so pooling works
		_, _ = io.Copy(ioutil.Discard, resp.Body)
		return resp, nil
	}
	var reader io.Reader = resp.Body

	if cl.StripBOM {
		reader = bom.NewReader(resp.Body)
	}

	if cd, ok := responseBody.(CustomDecoder); ok {
		return resp, cd.Decode(reader)
	}

	return resp, json.NewDecoder(reader).Decode(responseBody)
}

// ValidationErrors - this is a thin wrapper around the validator
// ValidationErrors type.  This makes a friendlier error message
// that attempts to interpret why validation failed and give
// a user friendly message.
type ValidationErrors struct {
	// The original unmolested ValidationErrors from the validator package
	OrigVE       validator.ValidationErrors
	parsedErrStr string
}

// Error - implement the Error interface.
func (ve ValidationErrors) Error() string {
	return ve.parsedErrStr
}

// make sense of the validator error types
func (cl *Client) validate(i interface{}) error {
	var err error
	rbv := reflect.ValueOf(i)
	rbvk := rbv.Kind()
	if rbvk == reflect.Slice || (rbvk == reflect.Ptr && rbv.Elem().Kind() == reflect.Slice) {
		if rbvk == reflect.Ptr {
			rbv = rbv.Elem()

		}
		for i := 0; i < rbv.Len(); i++ {
			err = validate.Struct(rbv.Index(i).Interface())
			if err != nil {
				break
			}
		}

	} else {
		err = validate.Struct(i)
	}
	if err != nil {
		if cl.rawValidatorErrors {
			return err
		}
		if verr, ok := err.(validator.ValidationErrors); ok {
			var errs []string
			for _, ferr := range verr {
				if ferr.ActualTag() == "required" {
					errs = append(errs,
						fmt.Sprintf("Required field %s is missing or empty",
							ferr.StructField(),
						),
					)
				} else if matches := altMatch.FindAllStringSubmatch(ferr.ActualTag(), -1); len(matches) > 0 {
					valids := make([]string, len(matches))
					for i := 0; i < len(matches); i++ {
						valids[i] = "\"" + matches[i][1] + "\""
					}
					errs = append(errs,
						fmt.Sprintf("Field '%s' invalid value: '%s', valid values are: %s",
							ferr.StructNamespace(),
							ferr.Value(), // for now all are string - revise this if other types are needed
							strings.Join(valids, ",")),
					)
				} else {
					errs = append(errs, fmt.Sprintf("Field '%s' invalid value: '%#v', validation tag was %s",
						ferr.StructNamespace(),
						ferr.Value(),
						ferr.ActualTag()))
				}
			}

			return ValidationErrors{
				OrigVE:       verr,
				parsedErrStr: fmt.Sprintf("Validation error: %s", strings.Join(errs, " ; ")),
			}
		}
	}
	return err
}

// ClientConfig - this configures an Client.
//
// Specify CACertBundlePath to load a bundle from disk to override the default.
// Specify CACertBundle if you want embed the cacert bundle in PEM format.
// Specify one or the other if you want to override, or neither to use the
// default.  If both are specified, CACertBundle is honored.
type ClientConfig struct {
	ClientTimeout      Duration
	CACertBundlePath   string
	CACertBundle       []byte
	InsecureSkipVerify bool
	Expiration         time.Time
	RawValidatorErrors bool // If true, then no attempt to interpret validator errors will be made.

	// FixupCallback - this is a method that will get called before every request
	// so that you can, for instance, manipulate headers for auth purposes, for
	// instance.
	FixupCallback FixupCallback
}

// Duration - this allows us to use a text representation of a duration and
// have it parse correctly.  The go standard library time.Duration does not
// implement the TextUnmarshaller interface, so we have to do this workaround
// in order for json.Unmarshal or external parsers like toml.Decode to work
// with human friendly input.
type Duration time.Duration

// UnmarshalText - this implements the TextUnmarshaler interface
func (d *Duration) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}
	dur, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

// MarshalText - this implements TextMarshaler
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// BaseClient - convenience wrapper for requests that all go to the same BaseURL.
type BaseClient struct {
	Client  *Client
	BaseURL *url.URL
}

// Get - like Client.Get, except uses the BaseClient.BaseURL instead of needing to
// be passed in.
func (bc *BaseClient) Get(ctx context.Context, path string, queryStruct interface{}, responseBody interface{}) error {
	_, err := bc.Client.Req(ctx, bc.BaseURL, "GET", path, queryStruct, nil, responseBody)
	return err
}

// Delete - like Client.Delete, except uses BaseClient.BaseURL instead of needing to
// be passed in.
func (bc *BaseClient) Delete(ctx context.Context, path string, queryStruct interface{}, responseBody interface{}) error {
	_, err := bc.Client.Req(ctx, bc.BaseURL, "DELETE", path, queryStruct, nil, responseBody)
	return err
}

// Post - like Client.Post, except uses BaseClient.BaseURL instead of needing to
// be passed in.
func (bc *BaseClient) Post(ctx context.Context, path string, queryStruct, requestBody interface{}, responseBody interface{}) error {
	_, err := bc.Client.Req(ctx, bc.BaseURL, "POST", path, queryStruct, requestBody, responseBody)
	return err
}

// Put - like Client.Put, except uses BaseClient.BaseURL instead of needing to
// be passed in.
func (bc *BaseClient) Put(ctx context.Context, path string, queryStruct, requestBody interface{}, responseBody interface{}) error {
	_, err := bc.Client.Req(ctx, bc.BaseURL, "PUT", path, queryStruct, requestBody, responseBody)
	return err
}

// Req - like Client.Req, except uses BaseClient.BaseURL instead of needing to be
// passed in.
func (bc *BaseClient) Req(ctx context.Context, method, path string, queryStruct,
	requestBody interface{}, responseBody interface{}) (*http.Response, error) {
	return bc.Client.Req(ctx, bc.BaseURL, method, path, queryStruct, requestBody, responseBody)
}

// ResponseError - this is an http response error type.  returned on >=400 status code.
type ResponseError struct {
	Status       string
	StatusCode   int
	ResponseBody []byte
	Header       http.Header
}

func (rs *ResponseError) Error() string {

	return fmt.Sprintf("response returned error status %d: %s with response payload: %s",
		rs.StatusCode,
		rs.Status,
		rs.ResponseBody,
	)
}
