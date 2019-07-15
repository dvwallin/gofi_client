package src

import (
	"fmt"
	"strings"
	"time"

	"github.com/dvwallin/gofi_client/src"
	"github.com/karrick/godirwalk"
)

func GetFiles() (err error) {

	var allFiles Files

	// using the TimeTrack function to meassure the time it took to execute
	// the file-fetching-function
	defer src.TimeTrack(time.Now(), "factorial")

	// initiating the variables needed for this function. some claim
	// we should short-hand -initiate variables as close to use
	// as possible but this looks pretty
	var (
		// tmp_on_external int = 0
		totalCount = 0
		// fileHash        string
		//		locFiles Files
		c = 0
	)

	// if externalName flag is not empty we want to flag the file
	// as located on an external drive
	// if *externalName != "" {
	// 	tmp_on_external = 1
	// }

	// some ui-logging
	src.Log(nil, "collecting file information ...")

	err = godirwalk.Walk(*rootDir, &godirwalk.Options{
		Callback: func(filePath string, de *godirwalk.Dirent) error {
			if err != nil {
				return err
			}
			split := strings.Split(filePath, "/")
			file := File{
				Name:  split[len(split)-1],
				Path:  strings.Replace(filePath, split[len(split)-1], "", -1),
				IsDir: src.IsDirAsInt(de.IsDir()),
			}
			if !*dryRun {
				allFiles = append(allFiles, file)
				if c > 4999 {
					src.Log(nil, fmt.Sprintf("found %dk files", totalCount))
					c = 0
				}
				totalCount++
				c++
				// jsonData, err := json.Marshal(file)

				// if err != nil {
				// 	log.Println(err)
				// }

				// jsonFile, err := os.Create(fmt.Sprintf("%s%s.tmp.JSON", tmpDir, helpers.RandStringBytesMaskImprSrcUnsafe(15)))

				// if err != nil {
				// 	log.Println(err)
				// }
				// defer jsonFile.Close()

				// _, err = jsonFile.Write(jsonData)
				// if err != nil {
				// 	log.Println(err)
				// }
				// jsonFile.Close()
			}
			return nil
		},
		Unsorted: true, // (optional) set true for faster yet non-deterministic enumeration (see godoc)
	})
	src.Log(err, "")

	return nil
}
