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

	"github.com/dvwallin/gofi_client/objects"
	"github.com/dvwallin/gofi_client/src"
	_ "github.com/mattn/go-sqlite3"
)

type (
	MyFileInfo struct {
		name string
		data []byte
	}
	MyFile struct {
		*bytes.Reader
		mif MyFileInfo
	}
)

const (
	BUFFERSIZE         = 2048
	SERVER_PORT        = 1985
	GOFI_DATABASE_NAME = "gofi.db"
	GOFI_LOG_FILE      = "gofi.log"
)

var (
	myIP  string
	myDir string
	err   error

	targetURL    *string = flag.String("target_url", fmt.Sprintf("127.0.0.1:%d", SERVER_PORT), "the URL where gofi_server is running")
	externalName *string = flag.String("external_name", "", "set this to a name of the external source to label it as external")
	rootDir      *string = flag.String("root_dir", ".", "which directory to start scanning in (then searches recursively)")
	hostname     *string = flag.String("hostname", "", "manually declare the hostname of the index")
	batchLimit   *int    = flag.Int("batchlimit", 5000, "number of files to write to each temporary json file. more memory can take higher limit")

	// debug-related flags
	cpuprof *string = flag.String("cpuprof", "", "the name of the cpuprof file")
	memprof *string = flag.String("memprof", "", "the name of the memprof file")

	db          *sql.DB
	stmt        *sql.Stmt
	storageFile string
	tmpDir      string
	reciever    *objects.Retriever
)

func (mif MyFileInfo) Name() string       { return mif.name }
func (mif MyFileInfo) Size() int64        { return int64(len(mif.data)) }
func (mif MyFileInfo) Mode() os.FileMode  { return 0444 }
func (mif MyFileInfo) ModTime() time.Time { return time.Time{} }
func (mif MyFileInfo) IsDir() bool        { return false }
func (mif MyFileInfo) Sys() interface{}   { return nil }

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

	if *hostname == "" {
		*hostname, err = os.Hostname()
		src.Log(err, "")
	}

	// creating our reciever object for global information
	reciever = &objects.Retriever{
		RootDir:      *rootDir,
		Hostname:     *hostname,
		IP:           myIP,
		ExternalName: *externalName,
		GGUID:        src.GetUUID(),
		BatchLimit:   batchLimit,
	}

	reciever.TmpDir = fmt.Sprintf("./%s/", reciever.GGUID)

	myDir, err = os.Getwd()
	src.Log(err, "")

	err = src.CreateDirIfNotExist(reciever.GGUID)
	src.Log(err, "")

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

	files := src.GetFiles(reciever)

	storageFile = fmt.Sprintf("./%s_%s", reciever.GGUID, GOFI_DATABASE_NAME)
	tmpDir = fmt.Sprintf("./%s/", reciever.GGUID)

	addToTmpDB(files) // Adding files from JSON -files to local db

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

func sendFileToServer(data []byte, id string, connection net.Conn) (err error) {
	defer connection.Close()

	mf := &MyFile{
		Reader: bytes.NewReader(data),
		mif: MyFileInfo{
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
	src.Log(err, "")

	_, err = connection.Write([]byte(fillestringFilename))
	src.Log(err, "")

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
		src.Log(err, "")
		sentBytes += BUFFERSIZE
	}
	log.Println("storageFile file has been sent ...")
	log.Println("file was", src.ByteCountSI(sentBytes), "bytes")
	if err := mf.Close(); err != nil {
		return err
	}
	return nil
}

func addToTmpDB(files []objects.File) {
	var totalCount int = 0

	log.Println("initiating saving files to database ...")

	matches, err := filepath.Glob(fmt.Sprintf("%s*.tmp.JSON", tmpDir))
	src.Log(err, "")

	// Connect to the database
	src.Log(nil, fmt.Sprintf("connecting to %s", storageFile))
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

	tx, err := db.Begin()
	src.Log(err, "")

	stmt, err = tx.Prepare("INSERT OR IGNORE INTO files(name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime, filehash, modified) values(?,?,?,?,?,?,?,?,?,?,?,?)")
	src.Log(err, "")

	for _, fv := range matches {
		jsonFile, err := os.Open(fv)
		src.Log(err, "")
		fmt.Println("processing", fv, "=>", storageFile)
		var partFiles []*objects.File
		byteValue, _ := ioutil.ReadAll(jsonFile)
		err = json.Unmarshal(byteValue, &partFiles)
		src.Log(err, "")
		for _, v := range partFiles {
			_, err = stmt.Exec(v.Name, v.Path, v.Size, v.IsDir, v.Machine, v.IP, v.OnExternalSource, v.ExternalName, v.FileType, v.FileMIME, v.FileHash, v.Modified)
			totalCount++
			src.Log(err, "")
		}

		jsonFile.Close()
	}
	log.Printf("\ncommitting %d files....\n", totalCount)
	err = tx.Commit()
	src.Log(err, "")
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
