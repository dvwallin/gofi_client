package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path"

	"github.com/davecgh/go-spew/spew"
	"github.com/gosuri/uiprogress"
	"github.com/iafan/cwalk"
	_ "github.com/mattn/go-sqlite3"
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
	Files []File
)

var (
	myIP       string
	myHostname string
	myDir      string
	err        error
	files      Files

	targetURL *string = flag.String("target_url", "127.0.0.1:1985", "the URL where gofi_server is running")
	dryRun    *bool   = flag.Bool("dry_run", false, "set this to true if the results should be printed and NOT sent to the server")
)

func main() {
	flag.Parse()

	myIP = getIP()
	myHostname, err = os.Hostname()
	if err != nil {
		log.Println(err)
	}

	myDir, err = os.Getwd()
	if err != nil {
		log.Println(err)
	}

	getFiles()
}

func CheckError(err error) {
	if err != nil {
		fmt.Println("Error: ", err)
	}
}

func conn(file File) bool {
	ServerAddr, err := net.ResolveUDPAddr("udp", *targetURL)
	CheckError(err)

	LocalAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:0", myIP))
	CheckError(err)

	Conn, err := net.DialUDP("udp", LocalAddr, ServerAddr)
	CheckError(err)
	defer Conn.Close()

	b, err := json.Marshal(file)
	if err != nil {
		log.Println(err)
		return false
	}
	_, err = Conn.Write(b)
	if err != nil {
		fmt.Println(file, err)
	}
	return true
}

func getIP() (ip string) {
	ip = "unknown"
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Println(err)
	}

	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip = ipnet.IP.String()
			}
		}
	}
	return
}

func getFiles() (foundFiles Files) {
	var files Files
	fmt.Println("collecting file information ...")
	err := cwalk.Walk(".",
		func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
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
		log.Println(err)
	}

	count := len(files)

	fmt.Println("found", count, "files ...")

	uiprogress.Start()
	bar := uiprogress.AddBar(count).AppendCompleted().PrependElapsed()
	bar.PrependFunc(func(b *uiprogress.Bar) string {
		return fmt.Sprintf("Sending file %d / %d", b.Current(), count)
	})

	for _, v := range files {
		conn(v)
		bar.Incr()
	}
	uiprogress.Stop()
	return
}
