package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
)

type csConnection struct {
	url      string
	username string
	password string
	ticket   string
}

const (
	apiBase      string = "/api/v1/"
	apiBaseNodes string = apiBase + "nodes/"
)

type docInfo struct {
	name     string
	filename string
	parentid int
}

type result struct {
	name string
	id   int
	err  error
}

type flagVar struct {
	parentID   int
	file       string
	prefix     string
	count      int
	url        string
	username   string
	password   string
	concurrent int
}

func initFlags(f *flagVar) *flagVar {
	flag.IntVar(&f.parentID, "parentid", 2000, "Parent ID of new documents")
	flag.StringVar(&f.file, "file", `c:\windows\win.ini`, "File to upload")
	flag.StringVar(&f.prefix, "name", "doc", "Document name prefix")
	flag.IntVar(&f.count, "count", 5, "Documents to upload")
	flag.StringVar(&f.url, "url", "", "Content Server URL")
	flag.StringVar(&f.username, "username", "Admin", "Username")
	flag.StringVar(&f.password, "password", "livelink", "Password")
	flag.IntVar(&f.concurrent, "concurrency", 5, "Number of concurrent uploads")
	return f
}

func main() {
	flags := &flagVar{}
	initFlags(flags)
	flag.Parse()

	if flags.file == "" {
		fmt.Println("You must specify a file")
		return
	} else if flags.url == "" {
		fmt.Println("You must specify a url")
		return
    }

	conn := newCSConnection(flags.url, flags.username, flags.password)
	fmt.Printf("Auth: %v\n", conn.auth())
	fmt.Printf("OTCSTicket: %s\n", conn.ticket)

	var parentID int
	parentID = flags.parentID

	guard := make(chan struct{}, flags.concurrent)

	wg := sync.WaitGroup{}

	for i := 0; i < flags.count; i++ {
		wg.Add(1)
		guard <- struct{}{} // write to guard as long as guard has room...

		go func(n int) {
			name := fmt.Sprintf("%s%05d", flags.prefix, n)
			_, err := conn.newDocument(name, flags.file, parentID)

			if err == nil {
				fmt.Printf("Added %s\n", name)
			} else {
				fmt.Printf("Added %s, error = %v\n", name, err)
			}
			<-guard // read from guard
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func newCSConnection(url, user, password string) *csConnection {
	return &csConnection{url, user, password, ""}
}

func (c *csConnection) newDocument(name string, filename string, parentID int) (int, error) {

	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)

	contentType, err := attachFile(bodyWriter, filename)

	if err != nil {
		return 0, nil
	}

	client := &http.Client{}

	args := map[string]string{
		"type":      "144",
		"name":      name,
		"parent_id": strconv.Itoa(parentID),
	}

	for k, v := range args {
		if err = bodyWriter.WriteField(k, v); err != nil {
			bodyWriter.Close()
			return 0, err
		}
	}

	bodyWriter.Close()

	req, err := http.NewRequest("POST", c.url+apiBaseNodes, bodyBuf)

	if err != nil {
		return 0, nil
	}

	// send ticket in header
	req.Header.Set("OTCSTicket", c.ticket)
	req.Header.Set("Content-Type", contentType)

	// We don't really care about the response, as long as the uplaod works.
	resp, err := client.Do(req)

	// read the whole body.
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

	if err != nil {
		return 0, err
	} else if resp.StatusCode != 200 {
		return 0, errors.New(resp.Status)
	}

	return 0, nil
}

func attachFile(bodyWriter *multipart.Writer, filename string) (string, error) {
	// write the file.
	fileWriter, err := bodyWriter.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}

	// open file handle
	fh, err := os.Open(filename)
	if err != nil {
		return "", err
	}

	// copy the file
	_, err = io.Copy(fileWriter, fh)
	if err != nil {
		return "", err
	}

	contentType := bodyWriter.FormDataContentType()

	return contentType, nil
}

func (c *csConnection) auth() error {
	vals := url.Values{}

	vals.Set("username", c.username)
	vals.Set("password", c.password)

	response, err := http.PostForm(c.url+apiBase+"auth", vals)

	if err != nil {
		return err
	}

	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)

	var authResponse = make(map[string]string)
	err = decoder.Decode(&authResponse)

	if err != nil {
		return err
	}

	ticket, ok := authResponse["ticket"]

	if !ok {
		return errors.New("No ticket in response")
	}

	c.ticket = ticket

	return nil
}
