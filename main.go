package main

import (
	"bytes"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/iafan/cwalk"
	_ "github.com/mattn/go-sqlite3"
	PUUID "github.com/pborman/uuid"
	pb "gopkg.in/cheggaaa/pb.v1"
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
)

var (
	myIP       string
	myHostname string
	myDir      string
	err        error
	files      Files
	b          []byte

	targetURL    *string = flag.String("target_url", fmt.Sprintf("127.0.0.1:%d", SERVER_PORT), "the URL where gofi_server is running")
	dryRun       *bool   = flag.Bool("dry_run", false, "set this to true if the results should be printed and NOT sent to the server")
	externalName *string = flag.String("external_name", "n/a", "set this to a name of the external source to label it as external")
	rootDir      *string = flag.String("root_dir", ".", "which directory to start scanning in (then searches recursively)")
	insertLimit  *int    = flag.Int("insert_limit", 5000, "number of files to write to db")

	db          *sql.DB
	stmt        *sql.Stmt
	res         sql.Result
	fileCount   int    = 0
	storageFile string = fmt.Sprintf("./%s_%s", getUUID(), GOFI_DATABASE_NAME)
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
			CONSTRAINT path_unique UNIQUE (path, machine, ip, onexternalsource, externalname)
			);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Printf("%q: %s\n", err, sqlStmt)
		return
	}
}

func main() {
	connection, err := net.Dial("tcp", *targetURL)
	if err != nil {
		panic(err)
	}
	defer connection.Close()

	_, err = getFiles()
	if err != nil {
		log.Println("error getting files", err)
	}

	// err = addFiles(files)
	// if err != nil {
	// 	log.Println("error adding files to local db", err)
	// }

	file, err := os.Open(storageFile)
	if err != nil {
		log.Println("error opening", storageFile, err)
	}

	b, err := ioutil.ReadAll(file)
	if err != nil {
		log.Println("error reading", storageFile, err)
	}

	sendFileToServer(b, getUUID(), connection) // Sending file to server

	deleteStorageFile() // Delete created storage file
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
	err = cwalk.Walk(*rootDir,
		func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			filePath = path.Join(myDir, filePath)
			file := File{
				Name:             info.Name(),
				Path:             filePath,
				Size:             info.Size(),
				Machine:          myHostname,
				IsDir:            0,
				IP:               myIP,
				OnExternalSource: tmp_on_external,
				ExternalName:     *externalName,
			}
			if info.IsDir() {
				file.IsDir = 1
			}
			if file.Name != "." && file.Name != ".." {
				if !*dryRun {
					if c > *insertLimit {
						log.Printf("\nfound %d files. writing to db..\n", *insertLimit)
						tx, err := db.Begin()
						if err != nil {
							log.Println(err)
						}

						for _, v := range files {
							stmt, err = tx.Prepare("INSERT OR IGNORE INTO files(name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime) values(?,?,?,?,?,?,?,?,?,?)")
							if err != nil {
								log.Println(err)
							}

							_, err = stmt.Exec(v.Name, v.Path, v.Size, v.IsDir, v.Machine, v.IP, v.OnExternalSource, v.ExternalName, v.FileType, v.FileMIME)

							if err != nil {
								log.Println(err)
							}
						}
						log.Printf("\ncommitting....\n")
						tx.Commit()
						files = Files{}
						c = 0
					}
					files = append(files, file)
					c++
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

	log.Println("\nfound", count, "files ...")

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

func deleteStorageFile() (err error) {
	err = os.Remove(storageFile)
	return
}

func addFiles(files Files) (err error) {

	log.Println("initiating saving files to database ...")

	count := len(files)

	log.Println("found", count, "files")

	bar := pb.StartNew(count)

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	for _, v := range files {
		bar.Increment()

		stmt, err = tx.Prepare("PRAGMA synchronous = OFF;INSERT OR IGNORE INTO files(name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime) values(?,?,?,?,?,?,?,?,?,?)")

		if err != nil {
			return err
		}

		_, err = stmt.Exec(v.Name, v.Path, v.Size, v.IsDir, v.Machine, v.IP, v.OnExternalSource, v.ExternalName, v.FileType, v.FileMIME)

		if err != nil {
			return err
		}
	}
	tx.Commit()
	bar.FinishPrint("done ...")

	return nil

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
