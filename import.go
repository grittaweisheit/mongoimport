package mongoimport

import (
	"fmt"
	"io"
	"sync"
	"time"

	// "context"

	"github.com/gosuri/uiprogress"
	"github.com/romnnn/mongoimport/loaders"
	"go.mongodb.org/mongo-driver/mongo"

	// "go.mongodb.org/mongo-driver/mongo/options"
	// "go.mongodb.org/mongo-driver/bson"
	log "github.com/sirupsen/logrus"
)

// Import ...
type Import struct {
	Connection   *MongoConnection
	Data         []*Datasource
	IgnoreErrors bool
}

// ImportResult ...
type ImportResult struct {
	Files      []string
	Collection string
	Succeeded  int
	Failed     int
	Elapsed    time.Duration
	errors     []error
}

// source *Datasource
// i Import
// bar *uiprogress.Bar
func (source *Datasource) importFiles(files []string, sourceWg *sync.WaitGroup, resultsChan chan ImportResult, bar *uiprogress.Bar, loader *loaders.Loader, collection *mongo.Collection, batchSize int, ignoreErrors bool) {
	defer sourceWg.Done()
	time.Sleep(1 * time.Second)

	// Check for hooks
	var postLoadHook PostLoadHook
	var preDumpHook PreDumpHook
	var updateFilter UpdateFilterHook

	if source.PostLoad != nil {
		postLoadHook = source.PostLoad
	} else {
		postLoadHook = defaultPostLoad
	}

	if source.PreDump != nil {
		preDumpHook = source.PreDump
	} else {
		preDumpHook = defaultPreDump
	}

	updateFilter = source.UpdateFilter
	log.Debugf("Update filter is %v", updateFilter)

	start := time.Now()
	result := ImportResult{
		Files:      files,
		Collection: source.Collection,
		Succeeded:  0,
		Failed:     0,
	}

	loader.Start(bar)

	batch := make([]interface{}, batchSize)
	batched := 0
	for {
		exit := false
		entry, err := loader.Load()
		if err != nil {
			switch err {
			case io.EOF:
				exit = true
			default:
				result.Failed++
				result.errors = append(result.errors, err)
				if ignoreErrors {
					log.Warnf(err.Error())
					continue
				} else {
					log.Errorf(err.Error())
					exit = true
				}
			}
		}

		if exit {
			// Insert remaining
			insert(collection, batch[:batched])
			break
		}

		// Apply post load hook
		loaded, err := postLoadHook(entry)
		if err != nil {
			log.Error(err)
			result.Failed++
			continue
		}

		// Apply pre dump hook
		dumped, err := preDumpHook(loaded)
		if err != nil {
			log.Error(err)
			result.Failed++
			continue
		}

		// Convert to BSON and add to batch
		batch[batched] = dumped
		batched++

		// Flush batch eventually
		if batched == batchSize {
			/*
				if updateFilter != nil {
					database.Collection(collection).UpdateMany(
						context.Background(),
						updateFilter(dumped), update, options.Update().SetUpsert(true),
					)
				}
			*/
			// database.Collection(collection).InsertMany(context.Background(), batch)
			// filter := bson.D{{}}
			// update := batch // []interface{}
			// options := options.UpdateOptions{}
			// options.se
			// log.Infof("insert into %s:%s", databaseName, collection)
			err := insert(collection, batch[:batched])
			if err != nil {
				log.Warn(err)
			}
			result.Succeeded += batched
			batched = 0
		}
	}
	loader.Finish()
	result.Elapsed = time.Since(start)
	resultsChan <- result
}

func (i Import) importSource(source *Datasource, wg *sync.WaitGroup, resultChan chan []ImportResult, db *mongo.Database) {
	defer wg.Done()
	var sourceWg sync.WaitGroup

	ldrs := make([]*loaders.Loader, len(source.Files))
	results := make([]ImportResult, len(source.Files))
	resultsChan := make(chan ImportResult, len(source.Files))
	updateChan := make(chan bool)

	batchSize := 100
	collection := db.Collection(source.Collection)

	bar := uiprogress.AddBar(10).AppendCompleted()
	// fmt.Println("One bar")
	if source.Type == loaders.MultipleInput {
		l, err := source.Loader.Create(source.Files)
		if err != nil {
			log.Fatalf("Failed to create loader for %d files: %s", len(source.Files), err.Error())
			return
		}
		bar.PrependFunc(i.progressStatus(source.Files, source.Collection, i.safePad()))
		bar.Set(0)
		sourceWg.Add(1)
		go source.importFiles(source.Files, &sourceWg, resultsChan, bar, l, collection, batchSize, i.IgnoreErrors)
	} else {
		for li, f := range source.Files {
			// Create a new loader for each file here
			l, err := source.Loader.Create([]string{f})
			if err != nil {
				log.Errorf("Skipping file %s because no loader could be created: %s", f, err.Error())
				continue
			}
			ldrs[li] = l
			bar.PrependFunc(i.progressStatus([]string{f}, source.Collection, i.safePad()))
			sourceWg.Add(1)
			go source.importFiles([]string{f}, &sourceWg, resultsChan, bar, ldrs[li], collection, batchSize, i.IgnoreErrors)
		}
	}

	go func() {
		for {
			exit := false
			select {
			case <-updateChan:
				exit = true
			case <-time.After(100 * time.Millisecond):
				for _, l := range ldrs {
					l.UpdateProgress()
				}
			}
			if exit {
				break
			}
		}
	}()

	sourceWg.Wait()
	close(updateChan)
	for _, l := range ldrs {
		// End prgress
		l.SetProgressPercent(1)
	}
	// Collect results
	for ri := range results {
		results[ri] = <-resultsChan
	}
	resultChan <- results
}

// Start ...
func (i Import) Start() (ImportResult, error) {
	var preWg sync.WaitGroup
	var importWg sync.WaitGroup
	uiprogress.Start()

	results := make([][]ImportResult, len(i.Data))
	resultsChan := make(chan []ImportResult, len(i.Data))

	start := time.Now()
	result := ImportResult{
		Succeeded: 0,
		Failed:    0,
	}

	dbClient, err := i.Connection.Client()
	if err != nil {
		return result, err
	}

	// Eventually empty collections
	needEmpty := make(map[string][]string)
	for _, source := range i.Data {
		if source.EmptyCollection {
			existingDatabases, willEmpty := needEmpty[source.Collection]
			newDatabase, err := i.databaseName(source)
			if err != nil {
				return result, fmt.Errorf("Missing database name for collection %s (%s): %s", source.Collection, source.Loader.Describe(), err.Error())
			}
			if !willEmpty || !contains(existingDatabases, newDatabase) {
				needEmpty[source.Collection] = append(existingDatabases, newDatabase)
			}
		}
	}
	for collectionName, collectionDatabases := range needEmpty {
		for _, db := range collectionDatabases {
			preWg.Add(1)
			go func(db string, collectionName string) {
				defer preWg.Done()
				log.Infof("Deleting all documents in %s:%s", db, collectionName)
				collection := dbClient.Database(db).Collection(collectionName)
				err := emptyCollection(collection)
				if err != nil {
					log.Warnf("Failed to delete all documents in collection %s:%s: %s", db, collectionName, err.Error())
				} else {
					log.Infof("Successfully deleted all documents in collection %s:%s", db, collectionName)
				}

			}(db, collectionName)
		}
	}

	// Wait for preprocessing to complete before starting to import
	preWg.Wait()
	for _, source := range i.Data {
		importWg.Add(1)
		db, err := i.databaseName(source)
		if err != nil {
			return result, err
		}
		go i.importSource(source, &importWg, resultsChan, dbClient.Database(db))
	}

	importWg.Wait()
	uiprogress.Stop()
	log.Info("Completed")
	for ri := range results {
		results[ri] = <-resultsChan
		for _, partResult := range results[ri] {
			result.Succeeded += partResult.Succeeded
			result.Failed += partResult.Failed
			log.Info(i.formattedResult(partResult))
		}
	}
	result.Elapsed = time.Since(start)
	return result, nil
}
