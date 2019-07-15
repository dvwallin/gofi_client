package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/dvwallin/gofi_client/src"
	_ "github.com/mattn/go-sqlite3"
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

	targetURL *string = flag.String("target_url", fmt.Sprintf("127.0.0.1:%d", SERVER_PORT), "the URL where gofi_server is running")
	//	externalName *string = flag.String("external_name", "", "set this to a name of the external source to label it as external")
	rootDir *string = flag.String("root_dir", ".", "which directory to start scanning in (then searches recursively)")
	//	hostname     *string = flag.String("hostname", generatedHostname, "manually declare the hostname of the index")

	// debug-related flags
	dryRun  *bool   = flag.Bool("dry_run", false, "set this to true if the results should be printed and NOT sent to the server")
	cpuprof *string = flag.String("cpuprof", "", "the name of the cpuprof file")
	memprof *string = flag.String("memprof", "", "the name of the memprof file")

	db          *sql.DB
	stmt        *sql.Stmt
	myGGUID     string = src.GetUUID()
	storageFile string = fmt.Sprintf("./%s_%s", myGGUID, GOFI_DATABASE_NAME)
	tmpDir      string = fmt.Sprintf("./%s/", myGGUID)
)

func (mif myFileInfo) Name() string       { return mif.name }
func (mif myFileInfo) Size() int64        { return int64(len(mif.data)) }
func (mif myFileInfo) Mode() os.FileMode  { return 0444 }
func (mif myFileInfo) ModTime() time.Time { return time.Time{} }
func (mif myFileInfo) IsDir() bool        { return false }
func (mif myFileInfo) Sys() interface{}   { return nil }

func (mf *MyFile) Close() error { return nil }

func (mf *MyFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, nil // not a directory but a single file
}

func (mf *MyFile) Stat() (os.FileInfo, error) {
	return mf.mif, nil
}

func init() {

	// initial random shite like creating a seed for later randomization
	// or parsing attribute flags
	rand.Seed(time.Now().UnixNano())
	flag.Parse()

	// fetching data for later use, such as ip, hostname, etc
	myIP, err = src.GetIP()
	src.Log(err, "")

	generatedHostname, err = os.Hostname()
	src.Log(err, "")

	myDir, err = os.Getwd()
	src.Log(err, "")

	err = src.CreateDirIfNotExist(myGGUID)
	src.Log(err, "")

	log.Println("connecting to", storageFile)

	// connecting to database
	db, err = sql.Open("sqlite3", storageFile)
	src.Log(err, "")

	// our schema for the temporary database
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
	src.Log(err, "")

	db.Close()
}

func main() {

	// when developing we might want profiling for cpu and/or memory.
	// by using the -cpuprof or -memprof argument flags we can generate
	// profiles for debugging.
	initiateProf()

	// putting all logging into a log file instead
	f, err := os.OpenFile(GOFI_LOG_FILE, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	src.Log(err, "")
	defer f.Close()
	log.SetOutput(f)

	// running through all files, in the rootDir, recursively
	// and fetches metadata of each file
	err = getFiles()
	src.Log(err, "")

	// addToTmpDB() // Adding files from JSON -files to local db

	// opening the storageFile which is later used for temporarily storing
	// all found files
	file, err := os.Open(storageFile)
	src.Log(err, "")

	// lets byte the hell out of the storageFile !!
	b, err := ioutil.ReadAll(file)
	src.Log(err, "")

	// now we should have all the files and an open storageFile
	// so let's connect to the server so we can send over the data
	connection, err := net.Dial("tcp", *targetURL)
	src.Log(err, "")
	defer connection.Close()

	// sending the files to the server for more permanent storage
	err = sendFileToServer(b, src.GetUUID(), connection)
	src.Log(err, "")

	// time to delete the temporary directory and the temporare storageFile
	// since it should all have sent to the server by now
	err = src.Remove(storageFile)
	src.Log(err, "")
	err = src.Remove(tmpDir)
	src.Log(err, "")

}

// func extractTypeMime(filePath string) (fileType string, fileMime string, fileHash string, avoidFurtherProcessing bool) {
// 	myFile, err := os.Open(filePath)
// 	if err != nil {
// 		log.Printf("failed to open the file: %v", err)
// 		avoidFurtherProcessing = true
// 	}
// 	defer myFile.Close()
// 	key, err := hex.DecodeString(GOFI_DEC_KEY)
// 	if err != nil {
// 		log.Printf("cannot decode hex key: %v", err)
// 	}
// 	hash, err := highwayhash.New(key)
// 	if err != nil {
// 		log.Printf("failed to create HighwayHash instance: %v", err)
// 	}
// 	if _, err = io.Copy(hash, myFile); err != nil {
// 		log.Printf("failed to read from file: %v", err)
// 	}
// 	fileHash = hex.EncodeToString(hash.Sum(nil))
// 	buf, _ := ioutil.ReadFile(filePath)

// 	kind, _ := filetype.Match(buf)
// 	fileType = kind.Extension
// 	fileMime = http.DetectContentType(buf)
// 	return fileType, fileMime, fileHash, avoidFurtherProcessing
// }

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

	fileSize := src.FillString(strconv.FormatInt(fileInfo.Size(), 10), 10)
	fillestringFilename := src.FillString(fileInfo.Name(), 64)

	log.Println("sending name and size of temporary file ...")
	_, err = connection.Write([]byte(fileSize))
	if err != nil {
		log.Println(err)
	}

	_, err = connection.Write([]byte(fillestringFilename))
	if err != nil {
		log.Println(err)
	}

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
		_, err = connection.Write(sendBuffer)
		if err != nil {
			log.Println(err)
		}
		sentBytes += BUFFERSIZE
	}
	log.Println("storageFile file has been sent ...")
	log.Println("file was", src.ByteCountSI(sentBytes), "bytes")
	if err := mf.Close(); err != nil {
		return err
	}
	return nil
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
		err = json.Unmarshal(byteValue, &partFiles)
		if err != nil {
			log.Println(err)
		}

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
	err = tx.Commit()
	if err != nil {
		log.Println(err)
	}
	db.Close()
}

func initiateProf() {
	if *cpuprof != "" {
		prof, err := os.Create("cpu.prof")
		if err != nil {
			log.Fatal(err)
		}
		err = pprof.StartCPUProfile(prof)
		if err != nil {
			panic(err)
		}
		defer pprof.StopCPUProfile()
	}

	if *memprof != "" {
		f, err := os.Create(*memprof)
		if err != nil {
			log.Fatal(err)
		}
		err = pprof.WriteHeapProfile(f)
		if err != nil {
			panic(err)
		}
		f.Close()
		return
	}
}

func statifyFiles() {
	// var (
	// 	modified               string
	// 	isDir                  int
	// 	fileType               string
	// 	fileMime               string
	// 	avoidFurtherProcessing bool
	// 	size                   int64 = 0
	// )

	// // check if we're handling a directory or not
	// // 	isDir = helpers.IsDirAsInt(de.IsDir())

	// if isDir == 0 {

	// 	if !avoidFurtherProcessing {
	// 		fileType, fileMime, fileHash, avoidFurtherProcessing = extractTypeMime(filePath)

	// 		stat, err := os.Stat(filePath)

	// 		if err != nil {
	// 			log.Println(err)
	// 			avoidFurtherProcessing = true
	// 		}

	// 		if !avoidFurtherProcessing {
	// 			modified = stat.ModTime().String()
	// 			size = stat.Size()
	// 		}
	// 	}
	// }
	// file := File{
	// 	Name:             de.Name(),
	// 	Path:             filePath,
	// 	Size:             size,
	// 	Machine:          *hostname,
	// 	IsDir:            0,
	// 	IP:               myIP,
	// 	OnExternalSource: tmp_on_external,
	// 	ExternalName:     *externalName,
	// 	FileType:         fileType,
	// 	FileMIME:         fileMime,
	// 	FileHash:         fileHash,
	// 	Modified:         modified,
	// }

	// if c > 4999 {
	// 	helpers.Log(nil, "found 5k files")
	// 	c = 0
	// }

	// c++
	// totalCount++

	// if *dryRun {
	// 	spew.Dump(file)
	// } else {
	// 	locFiles = append(locFiles, file)
	// }
}
