package src

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"
	"unsafe"

	PUUID "github.com/pborman/uuid"
)

var (
	err error
)

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

func FillString(retunString string, toLength int) string {
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

func GetUUID() (uuid string) {
	return PUUID.New()
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

func GetIP() (ip string, err error) {
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

func TimeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf("%s took %s", name, elapsed)
}

func Log(err error, msg string) {
	log.SetPrefix(time.Now().Format("2006-01-02 15:04:05"))
	if err != nil && msg == "" {
		log.Printf("%s\n", err)
	} else if err != nil && msg != "" {
		log.Printf("%s %s\n", err, msg)
	} else if err == nil && msg != "" {
		log.Printf("%s\n", msg)
	}
}

func IsDirAsInt(isDir bool) int {
	if isDir {
		return 1
	}
	return 0
}
