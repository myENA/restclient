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

// Client - admin api struct
type Client struct {
	Client             *http.Client
	rawValidatorErrors bool
	// Specifying this allows you to modify headers, add
	// auth tokens or signatures etc before the request is sent.
	FixupCallback FixupCallback

	// StripBOM - setting this to true gives you the option to strip
	// byte order markings from certain responses.
	StripBOM bool

	// FormEncodedBody - setting this to true uses x-www-form-urlencoded.
	// false (default) will do json encoding.
	FormEncodedBody bool
}

// CustomDecoder - If a response struct implements this interface,
// calls the Decode() method instead of json.Unmarshal.
type CustomDecoder interface {
	Decode(data io.Reader) error
}

// NewClient - Client factory method. - if transport is nil, build one
// using config data in cfg
func NewClient(cfg *ClientConfig, transport http.RoundTripper) (*Client, error) {
	c := &Client{}
	var err error

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

// Get - makes an http GET request to burl with path appended, and queryStruct optionally
// parsed by go-querystring and validated with go-playground/validator.v9.  Upon successful
// request, response is unmarshaled as json into responseBody, unless responseBody implements
// CustomDecoder, in which case Decode() is called.
func (cl *Client) Get(ctx context.Context, burl *url.URL, path string, queryStruct interface{}, responseBody interface{}) error {
	return cl.Req(ctx, burl, "GET", path, queryStruct, nil, responseBody)
}

// Delete - makes an http DELETE request to burl with path appended, and queryStruct optionally
// parsed by go-querystring and validated with go-playground/validator.v9.  Upon successful
// request, response is unmarshaled as json into responseBody, unless responseBody implements
// CustomDecoder, in which case Decode() is called.
func (cl *Client) Delete(ctx context.Context, burl *url.URL, path string, queryStruct interface{}, responseBody interface{}) error {
	return cl.Req(ctx, burl, "DELETE", path, queryStruct, nil, responseBody)
}

// Post - makes an http POST request to burl with path appended, and queryStruct optionally
// parsed by go-querystring and validated with go-playground/validator.v9.  requestBody is
// passed to go-playground/validator.v9 and is sent json-encoded as the body.  Upon successful
// request, response is unmarshaled as json into responseBody, unless responseBody implements
// CustomDecoder, in which case Decode() is called.
func (cl *Client) Post(ctx context.Context, burl *url.URL, path string, queryStruct, requestBody interface{}, responseBody interface{}) error {
	return cl.Req(ctx, burl, "POST", path, queryStruct, requestBody, responseBody)
}

// Put - makes an http PUT request to burl with path appended, and queryStruct optionally
// parsed by go-querystring and validated with go-playground/validator.v9.  requestBody is
// passed to go-playground/validator.v9 and is sent json-encoded as the body.  Upon successful
// request, response is unmarshaled as json into responseBody, unless responseBody implements
// CustomDecoder, in which case Decode() is called.
func (cl *Client) Put(ctx context.Context, burl *url.URL, path string, queryStruct, requestBody interface{}, responseBody interface{}) error {
	return cl.Req(ctx, burl, "PUT", path, queryStruct, requestBody, responseBody)
}

func isNil(i interface{}) bool {
	if i == nil {
		return true
	}
	v := reflect.ValueOf(i)
	switch v.Kind() {
	case reflect.Ptr:
		return v.IsNil()

	default:
		panic("Invalid interface type: " + v.Kind().String())
	}
}

// Req - like the method-specific versions above, this is the general purpose.
func (cl *Client) Req(ctx context.Context, burl *url.URL, method, path string, queryStruct, requestBody, responseBody interface{}) error {
	path = strings.TrimLeft(path, "/")
	finurl := burl.String() + "/" + path
	if !isNil(queryStruct) {
		err := cl.validate(queryStruct)
		if err != nil {
			return err
		}
		v, err := query.Values(queryStruct)
		if err != nil {
			return err
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
	if !isNil(requestBody) {
		err := cl.validate(requestBody)
		if err != nil {
			return err
		}
		if cl.FormEncodedBody {
			v, err := query.Values(requestBody)
			if err != nil {
				return err
			}
			bodyReader = strings.NewReader(v.Encode())
		} else {
			bjson, err := json.Marshal(requestBody)
			if err != nil {
				return err
			}
			bodyReader = bytes.NewReader(bjson)
		}
	}
	req, err := http.NewRequest(method, finurl, bodyReader)
	if err != nil {
		return err
	}

	req = req.WithContext(ctx)

	if cl.FormEncodedBody {
		req.Header["Content-Type"] = []string{"application/x-www-form-urlencoded"}
	} else {
		req.Header["Content-Type"] = []string{"application/json"}
	}

	if cl.FixupCallback != nil {
		err = cl.FixupCallback(req)
		if err != nil {
			return err
		}
	}
	resp, err := cl.Client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("invalid status code %d : %s : body: %s", resp.StatusCode, resp.Status, string(body))
	}
	if isNil(responseBody) {
		return nil
	}

	if cd, ok := responseBody.(CustomDecoder); ok {
		return cd.Decode(resp.Body)
	}

	var reader io.Reader = resp.Body
	if cl.StripBOM {
		reader = bom.NewReader(resp.Body)
	}

	return json.NewDecoder(reader).Decode(responseBody)
}

// make sense of the validator error types
func (cl *Client) validate(i interface{}) error {
	err := validate.Struct(i)
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

			return fmt.Errorf("Validation error: %s", strings.Join(errs, " ; "))
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
