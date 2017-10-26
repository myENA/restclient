package restclient

import (
	"testing"
	"net/http/httptest"
	"net/http"
	"io/ioutil"
	"os"
	"fmt"
	"net/url"
	"context"
	"encoding/json"
	"reflect"
)

var (
	server *httptest.Server
	client *Client
	th *testHandler
	u *url.URL
)

type testHandler struct {
	url *url.URL
	reqBody []byte
	header http.Header
	reqmethod string
	response []byte
	t *testing.T
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
	client.c = server.Client()
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
		UID: "testuid",
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
		UID: "testuid",
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
		UID: "testuid",
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