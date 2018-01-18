package restclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"
)

var (
	server *httptest.Server
	client *Client
	th     *testHandler
	u      *url.URL
)

type testHandler struct {
	url       *url.URL
	reqBody   []byte
	header    http.Header
	reqmethod string
	response  []byte
	t         *testing.T
}

func (th *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error
	defer r.Body.Close()
	th.header = make(http.Header)
	for key, headers := range r.Header {
		th.header[key] = make([]string, len(headers))
		copy(th.header[key], headers)
	}
	th.url = r.URL
	th.reqmethod = r.Method
	th.reqBody, err = ioutil.ReadAll(r.Body)
	th.t.Log(r.URL)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
	w.WriteHeader(200)
	w.Write(th.response)
}

func TestMain(m *testing.M) {
	th = &testHandler{}
	server = httptest.NewServer(th)
	var err error
	u, err = url.Parse(server.URL)
	if err != nil {
		panic(fmt.Sprintf("Failed on url.Parse call: %s, %s\n", server.URL, err))
	}
	client, err = NewClient(&ClientConfig{}, nil)
	if err != nil {
		panic(fmt.Sprintf("Failed on NewClient call: %s\n", err))
	}
	rc := m.Run()
	os.Exit(rc)
}

func TestGet(t *testing.T) {
	th.t = t
	var err error
	tr := &testResponse{
		Foo: "foo",
		Bar: "bar",
		Baz: 59,
	}
	th.response, err = json.Marshal(tr)
	if err != nil {
		t.Fatal("Error marshaling: ", err)
	}
	reqb := &testValidatorRequest{
		UID:     "testuid",
		KeyType: "s3",
	}
	tr2 := &testResponse{}
	err = client.Get(context.Background(), u, "/laterpath", reqb, tr2)
	if err != nil {
		t.Log("Failed client.Get: ", err)
		t.Fail()
	}
	if !reflect.DeepEqual(tr, tr2) {
		t.Log("data not preserved on round trip")
		t.Fail()
	}

}

func TestDelete(t *testing.T) {
	th.t = t
	var err error
	tr := &testResponse{
		Foo: "foo",
		Bar: "bar",
		Baz: 59,
	}
	th.response, err = json.Marshal(tr)
	if err != nil {
		t.Fatal("Error marshaling: ", err)
	}

	tr2 := &testResponse{}
	err = client.Delete(context.Background(), u, "/laterpath", nil, tr2)
	if err != nil {
		t.Log("Failed client.Delete: ", err)
		t.Fail()
	}
	if !reflect.DeepEqual(tr, tr2) {
		t.Log("data not preserved on round trip")
		t.Fail()
	}
}

func TestPost(t *testing.T) {
	th.t = t
	var err error
	tr := &testResponse{
		Foo: "foo",
		Bar: "bar",
		Baz: 59,
	}
	th.response, err = json.Marshal(tr)
	if err != nil {
		t.Fatal("Error marshaling: ", err)
	}

	tr2 := &testResponse{}
	reqb := &testValidatorRequest{
		UID:     "testuid",
		KeyType: "s3",
	}
	err = client.Post(context.Background(), u, "/whatever", nil, reqb, tr2)
	if err != nil {
		t.Log("Failed client.Post: ", err)
		t.Fail()
	}
	if !reflect.DeepEqual(tr, tr2) {
		t.Log("data not preserved on round trip")
		t.Fail()
	}

	// Not try with something we know will fail.

	reqb.UID = ""
	reqb.KeyType = "nots3"
	err = client.Post(context.Background(), u, "/whatever", nil, reqb, tr2)
	if err == nil {
		t.Log("error expected on failed validation, no error returned")
	} else {
		t.Log("expected validation error: ", err)
	}
}

func TestPut(t *testing.T) {
	th.t = t
	var err error
	tr := &testResponse{
		Foo: "foo",
		Bar: "bar",
		Baz: 59,
	}
	th.response, err = json.Marshal(tr)
	if err != nil {
		t.Fatal("Error marshaling: ", err)
	}

	tr2 := &testResponse{}
	reqb := &testValidatorRequest{
		UID:     "testuid",
		KeyType: "s3",
	}
	err = client.Put(context.Background(), u, "/whatever", nil, reqb, tr2)
	if err != nil {
		t.Log("Failed client.Put: ", err)
		t.Fail()
	}
	if !reflect.DeepEqual(tr, tr2) {
		t.Log("data not preserved on round trip")
		t.Fail()
	}
}

func TestPostSliceValidation(t *testing.T) {
	th.t = t
	var err error
	ttl := 100
	dreq := DNSRecords{
		DNSRecord{
			Type:     "A",
			Name:     "catpics.org",
			Data:     "1",
			Priority: nil,
			TTL:      &ttl,
			Service:  nil,
			Protocol: nil,
			Port:     nil,
			Weight:   nil,
		}, DNSRecord{
			Type:     "A",
			Name:     "catpics.org",
			Data:     "1",
			Priority: nil,
			TTL:      &ttl,
			Service:  nil,
			Protocol: nil,
			Port:     nil,
			Weight:   nil,
		},
	}

	err = client.Post(context.Background(), u, "/whatever", nil, dreq, nil)
	if err != nil {
		t.Log("error returned: " + err.Error())
		t.Fail()
	}

	// now make it fail
	dreq[1].Type = "ASDF"
	err = client.Post(context.Background(), u, "/whatever", nil, dreq, nil)

	if err == nil {
		t.Log("error was nil, should have failed validation")
		t.Fail()
	}
	if err.Error() != `Validation error: Field 'DNSRecord.Type' invalid value: 'ASDF', valid values are: "A","AAAA","CNAME","MX","NS","SOA","SRV","TXT"` {
		t.Log("unexpected error message, got ", err)
	}
}

type testValidatorRequest struct {
	UID         string `url:"uid" validate:"required"`
	SubUser     string `url:"subuser,omitempty"`
	AccessKey   string `url:"access-key,omitempty"`
	SecretKey   string `url:"secret-key,omitempty"`
	KeyType     string `url:"key-type,omitempty" validate:"omitempty,eq=s3|eq=swift"`
	GenerateKey *bool  `url:"generate-key,omitempty"` // defaults to true
}

type testResponse struct {
	Foo string
	Bar string
	Baz int
}

// DNSRecord represents an element of the GoDaddy model
// https://developer.godaddy.com/doc#!/_v1_domains/recordReplace/ArrayOfDNSRecord
type DNSRecord struct {
	Type     string  `json:"type" validate:"required,eq=A|eq=AAAA|eq=CNAME|eq=MX|eq=NS|eq=SOA|eq=SRV|eq=TXT"`
	Name     string  `json:"name" validate:"required,min=1,max=255"`
	Data     string  `json:"data" validate:"required,min=1,max=255"`
	Priority *int    `json:"priority,omitempty" validate:"omitempty,gte=1"`
	TTL      *int    `json:"ttl,omitempty" validate:"omitempty,gte=1"`
	Service  *string `json:"service,omitempty" validate:"omitempty,min=1"`
	Protocol *string `json:"protocol,omitempty" validate:"omitempty,min=1"`
	Port     *int    `json:"port,omitempty" validate:"omitempty,min=1,max=65535"`
	Weight   *int    `json:"weight,omitempty" validate:"omitempty,gte=1"`
}

// DNSRecords represents the GoDaddy model
// https://developer.godaddy.com/doc#!/_v1_domains/recordReplace/ArrayOfDNSRecord
type DNSRecords []DNSRecord
