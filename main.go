package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/iafan/cwalk"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sony/sonyflake"
)

type (
	File struct {
		ID      string `json:"id,omitempty"`
		Name    string `json:"name,omitempty"`
		Path    string `json:"path,omitempty"`
		Size    int64  `json:"size"`
		IsDir   int    `json:"isdir"`
		Machine string `json:"machine"`
		IP      string `json:"ip"`
	}
	Files      []File
	myFileInfo struct {
		name string
		data []byte
	}
	MyFile struct {
		*bytes.Reader
		mif myFileInfo
	}
)

const (
	BUFFERSIZE   = 2048
	SERVER_PORT  = 1985
	GOFI_TMP_DIR = ".gofi_tmp/"
)

var (
	myIP            string
	myHostname      string
	myDir           string
	err             error
	files           Files
	fileCountOutput int = 0

	targetURL *string = flag.String("target_url", fmt.Sprintf("127.0.0.1:%d", SERVER_PORT), "the URL where gofi_server is running")
	dryRun    *bool   = flag.Bool("dry_run", false, "set this to true if the results should be printed and NOT sent to the server")
)

func (mif myFileInfo) Name() string       { return mif.name }
func (mif myFileInfo) Size() int64        { return int64(len(mif.data)) }
func (mif myFileInfo) Mode() os.FileMode  { return 0444 }        // Read for all
func (mif myFileInfo) ModTime() time.Time { return time.Time{} } // Return anything
func (mif myFileInfo) IsDir() bool        { return false }
func (mif myFileInfo) Sys() interface{}   { return nil }

func (mf *MyFile) Close() error { return nil } // Noop, nothing to do

func (mf *MyFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, nil // We are not a directory but a single file
}

func (mf *MyFile) Stat() (os.FileInfo, error) {
	return mf.mif, nil
}

func init() {
	flag.Parse()

	myIP, err = getIP()
	if err != nil {
		log.Println("error getting client IP", err)
	}
	myHostname, err = os.Hostname()
	if err != nil {
		log.Println("error getting client hostname", err)
	}

	myDir, err = os.Getwd()
	if err != nil {
		log.Println(err)
	}
	// Create the GOFI_TMP_DIR in case it does not exist already
	newpath := filepath.Join(".", GOFI_TMP_DIR)
	os.MkdirAll(newpath, os.ModePerm)
}

func main() {
	connection, err := net.Dial("tcp", *targetURL)
	if err != nil {
		panic(err)
	}
	defer connection.Close()

	files, err := getFiles()
	if err != nil {
		log.Println("error getting files", err)
	}

	b, err := json.Marshal(files)
	if err != nil {
		log.Println("error marshaling files content", err)
	}

	id, err := genSonyflakeID()
	if err != nil {
		log.Println("error getting sonyflake id", err)
	}

	sendFileToServer(b, id, connection) // Sending file to server
}

func getIP() (ip string, err error) {
	ip = "unknown"
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ip, err
	}

	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip = ipnet.IP.String()
			}
		}
	}
	return ip, nil
}

func getFiles() (files Files, err error) {
	fmt.Println("collecting file information ...")
	err = cwalk.Walk(".",
		func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			fileCountOutput++
			if fileCountOutput/5000 == 1 {
				fmt.Printf("#")
				fileCountOutput = 0
			}
			filePath = path.Join(myDir, filePath)
			file := File{
				Name:    info.Name(),
				Path:    filePath,
				Size:    info.Size(),
				Machine: myHostname,
				IsDir:   0,
				IP:      myIP,
			}
			if info.IsDir() {
				file.IsDir = 1
			}
			if file.Name != "." && file.Name != ".." {
				if !*dryRun {
					files = append(files, file)
				} else {
					spew.Dump(file)
				}
			}
			return nil
		})
	if err != nil {
		return Files{}, err
	}

	count := len(files)

	fmt.Println("\nfound", count, "files ...")

	return files, nil
}

func sendFileToServer(data []byte, id uint64, connection net.Conn) (err error) {
	defer connection.Close()

	mf := &MyFile{
		Reader: bytes.NewReader(data),
		mif: myFileInfo{
			name: fmt.Sprintf("gofi_%d", id),
			data: data,
		},
	}

	fileInfo, err := mf.Stat()
	if err != nil {
		return err
	}

	fileSize := fillString(strconv.FormatInt(fileInfo.Size(), 10), 10)
	fillestringFilename := fillString(fileInfo.Name(), 64)

	fmt.Println("sending name and size of temporary file ...")
	connection.Write([]byte(fileSize))

	connection.Write([]byte(fillestringFilename))

	sendBuffer := make([]byte, BUFFERSIZE)
	fmt.Println("sending temporary ...")
	for {
		_, err = mf.Read(sendBuffer)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		connection.Write(sendBuffer)
	}
	fmt.Println("temporary file has been sent ...")
	if err := mf.Close(); err != nil {
		return err
	}
	fmt.Println("temporary file has been removed ...")
	return nil
}

func fillString(retunString string, toLength int) string {
	for {
		lengtString := len(retunString)
		if lengtString < toLength {
			retunString = retunString + ":"
			continue
		}
		break
	}
	return retunString
}

func genSonyflakeID() (id uint64, err error) {
	flake := sonyflake.NewSonyflake(sonyflake.Settings{})
	id, err = flake.NextID()
	if err != nil {
		return 0, err
	}
	return id, nil
}
