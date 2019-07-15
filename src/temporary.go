package src

import "os"

// creates gives directory if it does not exist
func CreateDirIfNotExist(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		return err
	}
	return err
}

// removes given directory or file
func Remove(path string) error {
	return os.Remove(path)
}
