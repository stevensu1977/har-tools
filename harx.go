package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type HarFile struct {
	Method   string
	URL      string
	Size     int
	MimeType string
}

type Har struct {
	Log HLog
}

type HLog struct {
	Version string
	Entries []HEntry
}

type HEntry struct {
	//StartedDateTime string
	//Time int
	Request  HRequest
	Response HResponse
}

type HRequest struct {
	Url    string
	Method string
}

type HResponse struct {
	Content HContent
}

type HContent struct {
	Size     int
	MimeType string
	Text     string
	Encoding string
}

// dump files to dir, keep hostname and path info as subfolders.
func (e *HEntry) dump(dir string) {
	u, err := url.Parse(e.Request.Url)
	if err != nil {
		log.Fatal(err)
	}
	scheme, host, path := u.Scheme, u.Host, u.Path // host,path = www.google.com , /search.do
	if scheme == "chrome-extension" {
		return // ignore all chrome-extension requests.
	}
	if i := strings.LastIndex(host, ":"); i != -1 {
		host = host[0:i] // remove port
	}
	path = dir + "/" + host + path
	if j := strings.LastIndex(path, "/"); j != -1 {
		os.MkdirAll(path[0:j], os.ModePerm)
		e.Response.Content.writeTo(path)
	}
}

// dump all files to the very dir, no subfolder created.
func (e *HEntry) dumpDirectly(dir string) {
	u, err := url.Parse(e.Request.Url)
	if err != nil {
		log.Fatal(err)
	}
	path := u.Path
	if j := strings.LastIndex(path, "/"); j != -1 {
		e.Response.Content.writeTo(dir + path[j:])
	}
}

func decode(str []byte, fileName string) {
	data, err := base64.StdEncoding.DecodeString(string(str))
	if err != nil {
		log.Fatal(err)
	} else {
		ioutil.WriteFile(fileName, data, os.ModePerm)
	}
}

func (c *HContent) writeTo(desiredFileName string) {
	f := getNoDuplicatePath(desiredFileName)

	if version12 { // Encoding is optional and added in Har spec v1.2
		if strings.EqualFold(c.Encoding, "base64") {
			decode([]byte(c.Text), f)
		} else {
			ioutil.WriteFile(f, []byte(c.Text), os.ModePerm)
		}
	} else {
		if strings.Index(c.MimeType, "text") != -1 || strings.Index(c.MimeType, "javascript") != -1 || strings.Index(c.MimeType, "json") != -1 {
			ioutil.WriteFile(f, []byte(c.Text), os.ModePerm)
		} else {
			decode([]byte(c.Text), f)
		}
	}
}

func fileExists(path string) bool {
	abs_path, _ := filepath.Abs(path)
	_, err := os.Stat(abs_path)
	return err == nil
}

func getNoDuplicatePath(desiredFileName string) (path string) {
	var i int
	for i, path = 2, desiredFileName; fileExists(path); i++ {

		ext := filepath.Ext(desiredFileName)
		pathBeforeExtension := strings.TrimSuffix(desiredFileName, ext)

		path = fmt.Sprintf("%s_(%d)%s", pathBeforeExtension, i, ext)
	}
	return
}

func (c *HContent) writeToFile(f *os.File) {
	f.WriteString(c.Text)
	f.Close()
}

func handle(r *bufio.Reader) {
	dec := json.NewDecoder(r)
	var har Har
	err := dec.Decode(&har)
	if err != nil {
		log.Fatal(err)
		os.Exit(-2)
	} else {
		if har.Log.Version == "1.2" {
			version12 = true
		}

		fmt.Printf("total %d entry \n", len(har.Log.Entries))

		for index, entry := range har.Log.Entries {
			output(index, entry)

		}
	}
}

func downloadURL(url string, parentPath string, fileName string) {

	mkPath(parentPath)
	res, err := http.Get(url)

	if err != nil {
		log.Fatalf("http.Get -> %v", err)
	}

	data, err := ioutil.ReadAll(res.Body)

	if err != nil {
		log.Fatalf("ioutil.ReadAll -> %v", err)
	}

	res.Body.Close()

	ioutil.WriteFile(parentPath+"/"+fileName, data, 0666)
	log.Printf("Completed ...[%s]", url)
	wg.Done()
}

func mkPath(path string) {

	if _, err := os.Stat(path); os.IsNotExist(err) {
		//fmt.Printf("file does not exist")
		os.MkdirAll(path, 0777)
	}

}

func addHarFile(harFile HarFile) {
	contxtPath := strings.Replace(harFile.URL, "https://", "", -1)
	contxtPath = strings.Replace(contxtPath, "http://", "", -1)
	parentPath := filepath.Dir(strings.Split(contxtPath, "?")[0])
	fileName := filepath.Base(strings.Split(contxtPath, "?")[0])
	wg.Add(1)
	go func() {
		//mkPath(filepath.Dir(strings.Split(contxtPath, "?")[0]))
		downloadURL(harFile.URL, parentPath, fileName)
	}()

	files = append(files, harFile)
}

func output(index int, entry HEntry) {
	if list {
		if urlPattern != nil {
			if len(urlPattern.FindString(entry.Request.Url)) > 0 {
				listEntries(index, entry)
			}
		} else if mimetypePattern != nil {
			if len(mimetypePattern.FindString(entry.Response.Content.MimeType)) > 0 {
				listEntries(index, entry)
			}
		} else {
			listEntries(index, entry)
		}
	} else if extract {
		if index == extractIndex {
			extractOne(entry)
		}
	} else if extractPattern {
		if urlPattern != nil && len(urlPattern.FindString(entry.Request.Url)) > 0 {
			entry.dump(dir)
		} else if mimetypePattern != nil && len(mimetypePattern.FindString(entry.Response.Content.MimeType)) > 0 {
			if dumpDirectly {
				entry.dumpDirectly(dir)
			} else {
				entry.dump(dir)
			}
		}
	} else if extractAll {
		entry.dump(dir)
	}
}

func listEntries(index int, entry HEntry) {
	harFile := HarFile{
		URL:      entry.Request.Url,
		Method:   entry.Request.Method,
		Size:     entry.Response.Content.Size,
		MimeType: entry.Response.Content.MimeType,
	}
	if download {
		addHarFile(harFile)
	} else {
		fmt.Printf("[%3d][%6s][%25s][Size:%8d][URL:%s]\n", index, entry.Request.Method, entry.Response.Content.MimeType, entry.Response.Content.Size, entry.Request.Url)
	}
}

func extractOne(entry HEntry) {
	fmt.Print(entry.Response.Content.Text)
}

var (
	files = make([]HarFile, 0)
	wg    sync.WaitGroup
)

var download bool = false

var list bool = false

var extract bool = false
var extractIndex int = -1
var dumpDirectly bool = false

var extractPattern bool = false
var urlPattern *regexp.Regexp = nil
var mimetypePattern *regexp.Regexp = nil

var extractAll bool = false
var dir string = ""

var version12 bool = false // is of version 1.2 format ?

func help() {
	fmt.Println(`
usage: harx [options] har-file
    -l                         List files , lead by [index]
    -lu  urlPattern            List files filtered by UrlPattern
    -lm  mimetypePattern       List files filtered by response Mimetype
    -x   dir                   eXtract all content to [dir]
    -xi  index                 eXtract the [index] content to stdout , need run with -l first to get [index]
    -xu  urlPattern      dir   eXtract to [dir] with UrlPattern filter
    -xm  mimetypePattern dir   eXtract to [dir] with MimetypePattern filter
    -xmd mimetypePattern dir   eXtract to [dir] Directly without hostname and path info, with MimetypePattern filter

    `)
}
func main() {
	if len(os.Args) == 1 {
		help()
		return
	}

	var fileName string
	switch os.Args[1] {
	case "-l":
		list = true
		fileName = os.Args[2]
	case "-ld":
		list = true
		download = true
		fileName = os.Args[2]
	case "-lu":
		list = true
		urlPattern = regexp.MustCompile(os.Args[2])
		fileName = os.Args[3]
	case "-lm":
		list = true
		mimetypePattern = regexp.MustCompile(os.Args[2])
		fileName = os.Args[3]
	case "-xi":
		extract = true
		extractIndex, _ = strconv.Atoi(os.Args[2])
		fileName = os.Args[3]
	case "-xu":
		extractPattern = true
		urlPattern = regexp.MustCompile(os.Args[2])
		dir = os.Args[3]
		fileName = os.Args[4]
	case "-xm":
		extractPattern = true
		mimetypePattern = regexp.MustCompile(os.Args[2])
		dir = os.Args[3]
		fileName = os.Args[4]
	case "-xmd":
		extractPattern = true
		dumpDirectly = true
		mimetypePattern = regexp.MustCompile(os.Args[2])
		dir = os.Args[3]
		fileName = os.Args[4]
	case "-x":
		extractAll = true
		dir = os.Args[2]
		fileName = os.Args[3]
	default:
		help()
		return
	}

	file, err := os.Open(fileName)

	if err == nil {
		if dir != "" {
			os.MkdirAll(dir, os.ModePerm)
		}
		handle(bufio.NewReader(file))
	} else {
		fmt.Printf("Cannot open file : %s\n", fileName)
		log.Fatal(err)
		os.Exit(-1)
	}
	wg.Wait()
}
