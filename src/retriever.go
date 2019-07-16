package src

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
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

var (
	fileSlice      []objects.File
	errorCount     int32
	primaryCounter int
	totalCount     int
	retriever      *objects.Retriever
)

func GetFiles(retrieverObject *objects.Retriever) []objects.File {
	retriever = retrieverObject
	err := filepath.Walk(retriever.RootDir, callback)
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
			if retriever.ExternalName != "" {
				external = 1
			}
			fullpath := strings.Replace(path, info.Name(), "", -1)

			var (
				filetype string = "file_to_large"
				filemime string = "file_to_large"
				filehash string = "file_to_large"
			)
			if info.Size() < 6442450944 {
				filetype, filemime, filehash = extractTypeMime(path)
			} else {
				Log(nil, fmt.Sprintf("%s is over 6gb", info.Name()))
			}
			file := objects.File{
				Name:             info.Name(),
				Path:             fullpath,
				Modified:         info.ModTime().String(),
				IsDir:            0,
				Machine:          retriever.Hostname,
				OnExternalSource: external,
				ExternalName:     retriever.ExternalName,
				FileType:         filetype,
				FileMIME:         filemime,
				FileHash:         filehash,
				Size:             info.Size(),
			}
			fileSlice = append(fileSlice, file)

			if len(fileSlice) > *retriever.BatchLimit-1 {
				jsonData, err := json.Marshal(fileSlice)

				Log(err, "")
				jsonFile, err := os.Create(fmt.Sprintf("%s%s%d.tmp.JSON", retriever.TmpDir, RandStringBytesMaskImprSrcUnsafe(5), primaryCounter))

				Log(err, "")
				primaryCounter++
				defer jsonFile.Close()

				_, err = jsonFile.Write(jsonData)
				Log(err, "")
				jsonFile.Close()
				Log(nil, fmt.Sprintf("%d files written to %s", len(fileSlice), jsonFile.Name()))
				totalCount = totalCount + len(fileSlice)
				fileSlice = nil
				fileSlice = []objects.File{}
				Log(nil, fmt.Sprintf("fileSlice now contains %d items", len(fileSlice)))
			}
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
