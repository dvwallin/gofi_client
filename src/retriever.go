package src

import (
	"encoding/hex"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/dvwallin/gofi_client/objects"
	"github.com/h2non/filetype"
	"github.com/minio/highwayhash"
)

var fileSlice []*objects.File
var errorCount int32
var rootPath string
var retrieverObject objects.Retriever

func GetFiles(retriever objects.Retriever) []*objects.File {
	retrieverObject = retriever
	err := filepath.Walk(retrieverObject.RootDir, callback)
	Log(err, "")
	return fileSlice
}

func callback(path string, info os.FileInfo, err error) error {
	if err != nil {
		atomic.AddInt32(&errorCount, 1)
		return err
	} else {
		if !info.IsDir() && info.Mode().IsRegular() {
			external := 0
			if retrieverObject.ExternalName != "" {
				external = 1
			}
			fullpath := strings.Replace(path, info.Name(), "", -1)
			filetype, filemime, filehash := extractTypeMime(path)
			file := objects.File{
				Name:             info.Name(),
				Path:             fullpath,
				Modified:         info.ModTime().String(),
				IsDir:            0,
				Machine:          retrieverObject.Hostname,
				OnExternalSource: external,
				ExternalName:     retrieverObject.ExternalName,
				FileType:         filetype,
				FileMIME:         filemime,
				FileHash:         filehash,
				Size:             info.Size(),
			}

			fileSlice = append(fileSlice, &file)
		}
	}
	return nil
}

func extractTypeMime(filePath string) (fileType string, fileMime string, fileHash string) {
	if _, err := os.Stat(filePath); err == nil {
		const GOFI_DEC_KEY = "000102030405060708090A0B0C0D0E0FF0E0D0C0B0A090807060504030201000"
		myFile, err := os.Open(filePath)
		Log(err, "")
		defer myFile.Close()
		key, err := hex.DecodeString(GOFI_DEC_KEY)
		Log(err, "")
		hash, err := highwayhash.New(key)
		Log(err, "")
		_, err = io.Copy(hash, myFile)
		Log(err, "")
		fileHash = hex.EncodeToString(hash.Sum(nil))
		buf, _ := ioutil.ReadFile(filePath)
		kind, _ := filetype.Match(buf)
		fileType = kind.Extension
		fileMime = http.DetectContentType(buf)
	}
	return fileType, fileMime, fileHash
}
