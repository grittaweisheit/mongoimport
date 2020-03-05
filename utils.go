package mongoimport

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uiprogress/util/strutil"
)

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func byteCountSI(b int64) string {
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

func (i Import) safePad() uint {
	var maxTotal, maxFilename, maxCollection string
	maxTotal = "589 GB" // Hardcoding seems sufficient
	for _, s := range i.Data {
		if len(s.Collection) > len(maxCollection) {
			maxCollection = s.Collection
		}
		for _, f := range s.Files {
			if filename := filepath.Base(f); len(filename) > len(maxFilename) {
				maxFilename = filename
			}
		}
	}
	return uint(len(i.formattedProgressStatus(maxFilename, maxCollection, maxTotal, maxTotal)) + 5)
}

func (i Import) formattedProgressStatus(filename string, collection string, bytesDone string, bytesTotal string) string {
	return fmt.Sprintf("[%s -> %s] %s/%s", filename, collection, bytesDone, bytesTotal)
}

func (i Import) formattedResult(result ImportResult) string {
	filename := filepath.Base(i.filenameOrSummary(result.Files))
	return fmt.Sprintf("[%s -> %s]: %d rows were imported successfully and %d failed in %s", filename, result.Collection, result.Succeeded, result.Failed, result.Elapsed)
}

func (i Import) filenameOrSummary(files []string) string {
	var filename string
	if len(files) == 1 {
		filename = filepath.Base(files[0])
	} else if len(files) > 1 {
		filename = fmt.Sprintf("[%d files]", len(files))
	}
	return filename
}

func (i Import) progressStatus(files []string, collection string, length uint) func(b *uiprogress.Bar) string {
	return func(b *uiprogress.Bar) string {
		filename := i.filenameOrSummary(files)
		bytesDone := byteCountSI(int64(b.Current()))
		bytesTotal := byteCountSI(int64(b.Total))
		status := i.formattedProgressStatus(filename, collection, bytesDone, bytesTotal)
		return strutil.Resize(status, length)
	}
}

func (i Import) databaseName(source *Datasource) (string, error) {
	databaseName := i.Connection.DatabaseName
	if source.DatabaseName != "" {
		databaseName = source.DatabaseName
	}
	if databaseName != "" {
		return databaseName, nil
	}
	return databaseName, errors.New("Missing database name")
}
