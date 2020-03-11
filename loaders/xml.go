package loaders

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// XMLLoader ...
type XMLLoader struct {
	readers    []io.Reader
	resultChan chan map[string]interface{}
	workerChan chan bool
	done       bool
	// entries
}

// DefaultXMLLoader ..
func DefaultXMLLoader() *XMLLoader {
	return &XMLLoader{}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Start ...
func (xmll *XMLLoader) Start() error {
	// entry, err := mxj.NewMapXmlReader(xmll.reader)
	xmll.done = false
	var wg sync.WaitGroup
	numFiles := len(xmll.readers)
	numParallelFiles := min(5, numFiles)
	jobChan := make(chan io.Reader, numFiles)
	xmll.resultChan = make(chan map[string]interface{}, numFiles)
	xmll.workerChan = make(chan bool)

	for w := 1; w <= numParallelFiles; w++ {
		wg.Add(1)
		go worker(w, &wg, jobChan, xmll.resultChan)
	}

	for _, f := range xmll.readers {
		jobChan <- f
	}
	close(jobChan)
	wg.Wait()
	xmll.done = true
	close(xmll.workerChan)
	return nil
}

func worker(id int, wg *sync.WaitGroup, jobs <-chan io.Reader, results chan<- map[string]interface{}) {
	defer wg.Done()
	for j := range jobs {
		// fmt.Println("worker", id, "started  job", j)
		time.Sleep(time.Second)
		// fmt.Println("worker", id, "finished job", j)
		results <- map[string]interface{}{"Hallo": j}
	}
}

// Describe ...
func (xmll *XMLLoader) Describe() string {
	return "XML"
}

// Finish ...
func (xmll *XMLLoader) Finish() error {
	return nil
}

// Create ...
func (xmll XMLLoader) Create(readers []io.Reader, skipSanitization bool) (ImportLoader, error) {
	if len(readers) < 1 {
		return nil, fmt.Errorf("XML reader needs at least one input file")
	}
	return &XMLLoader{
		readers: readers,
	}, nil
}

// Type ...
func (xmll *XMLLoader) Type() InputType {
	return MultipleInput
}

// Load ...
func (xmll *XMLLoader) Load() (map[string]interface{}, error) {
	/*
		entry, err := mxj.NewMapXmlReader(reader)
		if err != nil {
			return nil, err
		}
		return entry, nil
	*/
	/*
		select {
		case <-xmll.workerChan:
			// All workers are done

		}
	*/

	for {
		select {
		case result := <-xmll.resultChan:
			// fmt.Println("WaitGroup finished!")
			return result, nil
		case <-time.After(100 * time.Millisecond):
			fmt.Println("WaitGroup timed out..")
		}
		if xmll.done {
			break
		}
	}
	return nil, io.EOF
}
