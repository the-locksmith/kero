package loader

import (
	"fmt"
	"github.com/galaco/bsp"
	"github.com/galaco/bsp/lumps"
	"github.com/galaco/kero/framework/console"
	"github.com/galaco/kero/framework/graphics"
	"github.com/galaco/kero/framework/graphics/studiomodel"
	"strings"
	"sync"
)

func LoadStaticProps(fs graphics.VirtualFileSystem, file *bsp.Bsp) (map[string]*graphics.Model, []graphics.StaticProp) {
	gameLump := file.Lump(bsp.LumpGame).(*lumps.Game)
	propLump := gameLump.GetStaticPropLump()

	// Get StaticProp list to load
	propPaths := make([]string, 0)
	for _, propEntry := range propLump.PropLumps {
		propPaths = append(propPaths, propLump.DictLump.Name[propEntry.GetPropType()])
	}
	propPaths = generateUniquePropList(propPaths)
	console.PrintString(console.LevelInfo, fmt.Sprintf("%d staticprops referenced", len(propPaths)))

	// Load Prop data
	propDictionary := asyncLoadProps(fs, propPaths)
	console.PrintString(console.LevelSuccess, fmt.Sprintf("%d staticprops loaded", len(propDictionary)))

	if len(propDictionary) != len(propPaths) {
		console.PrintString(console.LevelError, fmt.Sprintf("%d staticprops could not be loaded", len(propPaths)-len(propDictionary)))
	}

	//Transform to props to
	staticPropList := make([]graphics.StaticProp, 0)

	for _, propEntry := range propLump.PropLumps {
		modelName := propLump.DictLump.Name[propEntry.GetPropType()]
		if m, ok := propDictionary[modelName]; ok {
			staticPropList = append(staticPropList, *graphics.NewStaticProp(propEntry, &propLump.LeafLump, m))
			continue
		} else {
			// TODO error Prop
		}
	}

	return propDictionary, staticPropList
}

func generateUniquePropList(propList []string) (uniqueList []string) {
	list := map[string]bool{}
	for _, entry := range propList {
		if _, ok := list[entry]; !ok {
			list[entry] = true
			uniqueList = append(uniqueList, entry)
		}
	}

	return uniqueList
}

func asyncLoadProps(fs graphics.VirtualFileSystem, propPaths []string) map[string]*graphics.Model {
	propMap := map[string]*graphics.Model{}
	var propMapMutex sync.Mutex
	waitGroup := sync.WaitGroup{}

	asyncLoadProp := func(path string) {
		defer func() {
			if e := recover(); e != nil {
				console.PrintString(console.LevelError, e.(error).Error())
			}
		}()
		if !strings.HasSuffix(path, ".mdl") {
			path += ".mdl"
		}
		prop, err := studiomodel.LoadProp(path, fs)
		if err != nil {
			waitGroup.Done()
			console.PrintString(console.LevelError, fmt.Sprintf("Error loading prop '%s': %s", path, err.Error()))
			return
		}
		propMapMutex.Lock()
		propMap[path] = prop
		propMapMutex.Unlock()
		waitGroup.Done()
	}

	waitGroup.Add(len(propPaths))
	for _, path := range propPaths {
		go asyncLoadProp(path)
	}
	waitGroup.Wait()

	return propMap
}
