package main

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unsafe"

	"github.com/davecgh/go-spew/spew"
	"github.com/h2non/filetype"
	"github.com/karrick/godirwalk"
	_ "github.com/mattn/go-sqlite3"
	"github.com/minio/highwayhash"
	PUUID "github.com/pborman/uuid"
)

type (
	File struct {
		ID               string `json:"id,omitempty"`
		Name             string `json:"name,omitempty"`
		Path             string `json:"path,omitempty"`
		Size             int64  `json:"size"`
		IsDir            int    `json:"isdir"`
		Machine          string `json:"machine"`
		IP               string `json:"ip"`
		OnExternalSource int    `json:"on_external_source"`
		ExternalName     string `json:"external_name"`
		FileType         string `json:"file_type"`
		FileMIME         string `json:"file_mime"`
		FileHash         string `json:"file_hash"`
		Modified         string `json:"modified"`
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
	BUFFERSIZE         = 2048
	SERVER_PORT        = 1985
	GOFI_DATABASE_NAME = "gofi.db"
	GOFI_DEC_KEY       = "000102030405060708090A0B0C0D0E0FF0E0D0C0B0A090807060504030201000"
	GOFI_LOG_FILE      = "gofi.log"
)

var (
	myIP              string
	generatedHostname string
	myDir             string
	err               error
	files             Files
	b                 []byte

	targetURL    *string = flag.String("target_url", fmt.Sprintf("127.0.0.1:%d", SERVER_PORT), "the URL where gofi_server is running")
	dryRun       *bool   = flag.Bool("dry_run", false, "set this to true if the results should be printed and NOT sent to the server")
	externalName *string = flag.String("external_name", "n/a", "set this to a name of the external source to label it as external")
	rootDir      *string = flag.String("root_dir", ".", "which directory to start scanning in (then searches recursively)")
	insertLimit  *int    = flag.Int("insert_limit", 5000, "number of files to write to db")
	hostname     *string = flag.String("hostname", generatedHostname, "manually declare the hostname of the index")

	db          *sql.DB
	stmt        *sql.Stmt
	res         sql.Result
	fileCount   int    = 0
	myGGUID     string = getUUID()
	storageFile string = fmt.Sprintf("./%s_%s", myGGUID, GOFI_DATABASE_NAME)
	tmpDir      string = fmt.Sprintf("./%s/", myGGUID)
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

	rand.Seed(time.Now().UnixNano())

	flag.Parse()

	myIP, err = getIP()
	if err != nil {
		log.Println("error getting client IP", err)
	}
	generatedHostname, err = os.Hostname()
	if err != nil {
		log.Println("error getting client hostname", err)
	}

	myDir, err = os.Getwd()
	if err != nil {
		log.Println(err)
	}

	err = CreateDirIfNotExist(myGGUID)
	if err != nil {
		log.Println(err)
	}

	log.Println("connecting to", storageFile)

	// Connect to the database
	db, err = sql.Open("sqlite3", storageFile)
	if err != nil {
		log.Println(err)
	}

	// Make sure the correct scheme exists
	sqlStmt := `
		CREATE TABLE IF NOT EXISTS files 
			(	id integer NOT NULL primary key, 
				name text NOT NULL, 
				path text NOT NULL, 
				size integer NOT NULL, 
				isdir integer NOT NULL, 
				machine text NOT NULL, 
				ip text NOT NULL,
				onexternalsource integer NOT NULL,
				externalname text NOT NULL,
				filetype text NOT NULL,
				filemime text NOT NULL,
        filehash text NOT NULL,
        modified text NOT NULL,
			CONSTRAINT path_unique UNIQUE (path, machine, ip, onexternalsource, externalname, filehash)
			);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Printf("%q: %s\n", err, sqlStmt)
		return
	}

	db.Close()
}

func main() {
	f, err := os.OpenFile(GOFI_LOG_FILE, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	log.SetOutput(f)
	log.Printf("--\ninitiating gofi client..\n--\n\n")

	_, err = getFiles()
	if err != nil {
		log.Println("error getting files", err)
	}

	addToTmpDB() // Adding files from JSON -files to local db

	file, err := os.Open(storageFile)
	if err != nil {
		log.Println("error opening", storageFile, err)
	}

	b, err := ioutil.ReadAll(file)
	if err != nil {
		log.Println("error reading", storageFile, err)
	}

	connection, err := net.Dial("tcp", *targetURL)
	if err != nil {
		panic(err)
	}
	defer connection.Close()

	sendFileToServer(b, getUUID(), connection) // Sending file to server

	deleteLoc(storageFile) // Delete created storage file
	deleteLoc(tmpDir)      // Delete created tmp dir
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
	var (
		tmp_on_external int = 0
	)
	if *externalName != "n/a" {
		tmp_on_external = 1
	}
	log.Println("collecting file information ...")
	c := 0
	fc := 0
	totalCount := 0

	err = godirwalk.Walk(*rootDir, &godirwalk.Options{
		Callback: func(filePath string, de *godirwalk.Dirent) error {
			if err != nil {
				return err
			}

			var (
				fileHash                    string
				modified                    string
				isDir                       int = 0
				fileType                    string
				fileMime                    string
				avoidDetailedFileProcessing bool
				size                        int64 = 0
			)
			if de.IsDir() {
				isDir = 1
			}
			if isDir < 1 {
				myFile, err := os.Open(filePath)
				if err != nil {
					log.Printf("failed to open the file: %v", err)
					avoidDetailedFileProcessing = true
				}
				defer myFile.Close()
				if !avoidDetailedFileProcessing {
					key, err := hex.DecodeString(GOFI_DEC_KEY)
					if err != nil {
						log.Printf("cannot decode hex key: %v", err)
					}
					hash, err := highwayhash.New(key)
					if err != nil {
						log.Printf("failed to create HighwayHash instance: %v", err)
					}
					if _, err = io.Copy(hash, myFile); err != nil {
						log.Printf("failed to read from file: %v", err)
					}

					stat, err := os.Stat(filePath)
					spew.Dump(stat)
					if err != nil {
						log.Println(err)
					}
					fileHash = hex.EncodeToString(hash.Sum(nil))
					modified = fmt.Sprintf("%s", stat.ModTime())
					buf, _ := ioutil.ReadFile(filePath)

					kind, _ := filetype.Match(buf)
					if kind == filetype.Unknown {
						log.Println("Unknown file type")
					}
					fileType = kind.Extension
					fileMime = http.DetectContentType(buf)
					size = stat.Size()
				}
			}
			file := File{
				Name:             de.Name(),
				Path:             filePath,
				Size:             size,
				Machine:          *hostname,
				IsDir:            0,
				IP:               myIP,
				OnExternalSource: tmp_on_external,
				ExternalName:     *externalName,
				FileType:         fileType,
				FileMIME:         fileMime,
				FileHash:         fileHash,
				Modified:         modified,
			}
			spew.Dump(file)
			return nil
			if file.Name != "." && file.Name != ".." {
				if !*dryRun {
					if c > *insertLimit {
						jsonData, err := json.Marshal(files)

						if err != nil {
							log.Println(err)
						}

						jsonFile, err := os.Create(fmt.Sprintf("%s%s%d.tmp.JSON", tmpDir, RandStringBytesMaskImprSrcUnsafe(5), fc))

						if err != nil {
							log.Println(err)
						}
						fc++
						defer jsonFile.Close()

						jsonFile.Write(jsonData)
						jsonFile.Close()
						fmt.Println(len(files), "files written to ", jsonFile.Name())
						log.Println(len(files), "files written to ", jsonFile.Name())
						totalCount = totalCount + len(files)
						c = 0
						files = Files{}
					}
					files = append(files, file)
					c++
				} else {
					spew.Dump(file)
				}
			}
			return nil
		},
		Unsorted: true, // (optional) set true for faster yet non-deterministic enumeration (see godoc)
	})
	if err != nil {
		return Files{}, err
	}

	return files, nil
}

func sendFileToServer(data []byte, id string, connection net.Conn) (err error) {
	defer connection.Close()

	mf := &MyFile{
		Reader: bytes.NewReader(data),
		mif: myFileInfo{
			name: fmt.Sprintf("gofi_%s.db", id),
			data: data,
		},
	}

	fileInfo, err := mf.Stat()
	if err != nil {
		return err
	}

	fileSize := fillString(strconv.FormatInt(fileInfo.Size(), 10), 10)
	fillestringFilename := fillString(fileInfo.Name(), 64)

	log.Println("sending name and size of temporary file ...")
	connection.Write([]byte(fileSize))

	connection.Write([]byte(fillestringFilename))

	sendBuffer := make([]byte, BUFFERSIZE)
	var sentBytes int64
	log.Println("sending temporary ...")
	for {
		_, err = mf.Read(sendBuffer)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		connection.Write(sendBuffer)
		sentBytes += BUFFERSIZE
	}
	log.Println("storageFile file has been sent ...")
	log.Println("file was", ByteCountSI(sentBytes), "bytes")
	if err := mf.Close(); err != nil {
		return err
	}
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

func getUUID() (uuid string) {
	return PUUID.New()
}

func deleteLoc(path string) (err error) {
	err = os.Remove(path)
	return
}

func ByteCountSI(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

func addToTmpDB() {
	var totalCount int = 0
	log.Println("initiating saving files to database ...")

	matches, err := filepath.Glob(fmt.Sprintf("%s*.tmp.JSON", tmpDir))

	if err != nil {
		fmt.Println(err)
	}

	// Connect to the database
	db, err = sql.Open("sqlite3", storageFile)
	if err != nil {
		log.Println(err)
	}
	tx, err := db.Begin()
	if err != nil {
		log.Println(err)
	}

	stmt, err = tx.Prepare("INSERT OR IGNORE INTO files(name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime, filehash, modified) values(?,?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		log.Println(err)
	}

	for _, fv := range matches {
		jsonFile, err := os.Open(fv)

		if err != nil {
			log.Println(err)
		}
		fmt.Println("processing", fv, "=>", storageFile)
		var partFiles Files
		byteValue, _ := ioutil.ReadAll(jsonFile)
		json.Unmarshal(byteValue, &partFiles)

		for _, v := range partFiles {
			_, err = stmt.Exec(v.Name, v.Path, v.Size, v.IsDir, v.Machine, v.IP, v.OnExternalSource, v.ExternalName, v.FileType, v.FileMIME, v.FileHash, v.Modified)
			totalCount++

			if err != nil {
				log.Println(err)
			}
		}

		jsonFile.Close()
	}
	log.Printf("\ncommitting %d files....\n", totalCount)
	tx.Commit()
	db.Close()
}

func CreateDirIfNotExist(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		return err
	}
	return err
}

func RandStringBytesMaskImprSrcUnsafe(n int) string {
	const (
		letterBytes   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
		letterIdxBits = 6
		letterIdxMask = 1<<letterIdxBits - 1
		letterIdxMax  = 63 / letterIdxBits
	)
	var src = rand.NewSource(time.Now().UnixNano())
	b := make([]byte, n)
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return *(*string)(unsafe.Pointer(&b))
}
