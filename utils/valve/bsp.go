package valve

import (
	"fmt"
	"github.com/galaco/bsp"
	"github.com/galaco/bsp/lumps"
	"github.com/galaco/bsp/primitives/common"
	"github.com/galaco/bsp/primitives/dispinfo"
	"github.com/galaco/bsp/primitives/dispvert"
	"github.com/galaco/bsp/primitives/face"
	"github.com/galaco/bsp/primitives/plane"
	"github.com/galaco/bsp/primitives/texinfo"
	"github.com/galaco/kero/framework/console"
	"github.com/galaco/kero/framework/filesystem"
	"github.com/galaco/kero/framework/graphics"
	graphics3d "github.com/galaco/kero/framework/graphics/3d"
	"github.com/galaco/vtf/format"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/golang-source-engine/stringtable"
	"math"
	"strings"
	"sync"
)

const (
	ErrorName = "error"
)

type bspstructs struct {
	faces     []face.Face
	planes    []plane.Plane
	vertexes  []mgl32.Vec3
	surfEdges []int32
	edges     [][2]uint16
	texInfos  []texinfo.TexInfo
	dispInfos []dispinfo.DispInfo
	dispVerts []dispvert.DispVert
	lightmap  []common.ColorRGBExponent32
}

// LoadBspMap is the gateway into loading the core static level. Entities are loaded
// elsewhere
// It loads in the following order:
// BSP Geometry
// BSP Materials
// StaticProps (materials loaded as required)
func LoadBSPWorld(fs filesystem.FileSystem, file *bsp.Bsp) (*Bsp, error) {
	bspStructure := bspstructs{
		faces:     file.Lump(bsp.LumpFaces).(*lumps.Face).GetData(),
		planes:    file.Lump(bsp.LumpPlanes).(*lumps.Planes).GetData(),
		vertexes:  file.Lump(bsp.LumpVertexes).(*lumps.Vertex).GetData(),
		surfEdges: file.Lump(bsp.LumpSurfEdges).(*lumps.Surfedge).GetData(),
		edges:     file.Lump(bsp.LumpEdges).(*lumps.Edge).GetData(),
		texInfos:  file.Lump(bsp.LumpTexInfo).(*lumps.TexInfo).GetData(),
		dispInfos: file.Lump(bsp.LumpDispInfo).(*lumps.DispInfo).GetData(),
		dispVerts: file.Lump(bsp.LumpDispVerts).(*lumps.DispVert).GetData(),
		lightmap:  file.Lump(bsp.LumpLighting).(*lumps.Lighting).GetData(),
	}

	//MATERIALS
	stringTable := stringtable.NewFromExistingStringTableData(
		file.Lump(bsp.LumpTexDataStringData).(*lumps.TexDataStringData).GetData(),
		file.Lump(bsp.LumpTexDataStringTable).(*lumps.TexDataStringTable).GetData())
	materials := buildUniqueMaterialList(stringTable, &bspStructure.texInfos)

	materialDictionary := buildMaterialDictionary(fs, materials)

	// BSP FACES
	bspMesh := graphics.NewMesh()
	bspFaces := make([]BspFace, len(bspStructure.faces))
	// storeDispFaces until for visibility calculation purposes.
	dispFaces := make([]int, 0)

	var lightmapAtlas *graphics.TextureAtlas
	if bspStructure.lightmap != nil {
		lightmapAtlas = GenerateLightmapTexture(bspStructure.faces, bspStructure.lightmap)
	}

	for idx, f := range bspStructure.faces {
		if f.DispInfo > -1 {
			// This face is a displacement
			bspFaces[idx] = generateDisplacementFace(&f, &bspStructure, bspMesh)
			dispFaces = append(dispFaces, idx)
		} else {
			bspFaces[idx] = generateBspFace(&bspStructure.faces[idx], &bspStructure, bspMesh)
		}

		faceVmt, err := stringTable.FindString(int(bspStructure.texInfos[bspStructure.faces[idx].TexInfo].TexData))
		if err != nil {
			console.PrintInterface(console.LevelError, err)
		} else {
			bspFaces[idx].SetMaterial(strings.ToLower(faceVmt))
		}
	}

	return NewBsp(file, bspMesh, bspFaces, dispFaces, materialDictionary, bspStructure.texInfos, lightmapAtlas), nil
}

// SortUnique builds a unique list of materials in a StringTable
// referenced by BSP TexInfo lump data.
func buildUniqueMaterialList(stringTable *stringtable.StringTable, texInfos *[]texinfo.TexInfo) []string {
	materialList := make([]string, 0)
	for _, ti := range *texInfos {
		target, _ := stringTable.FindString(int(ti.TexData))
		found := false
		for _, cur := range materialList {
			if cur == target {
				found = true
				break
			}
		}
		if !found {
			materialList = append(materialList, target)
		}
	}

	return materialList
}

func buildMaterialDictionary(fs filesystem.FileSystem, materials []string) (dictionary map[string]*graphics.Material) {
	dictionary = map[string]*graphics.Material{}
	waitGroup := sync.WaitGroup{}
	dictMutex := sync.Mutex{}

	asyncLoadMaterial := func(filePath string) {
		mat, err := graphics.LoadMaterial(fs, filePath)
		if err != nil {
			console.PrintString(console.LevelError, fmt.Sprintf("%s", err))
			mat = graphics.NewMaterial(filePath)
		}
		dictMutex.Lock()
		dictionary[strings.ToLower(filePath)] = mat
		dictMutex.Unlock()
		waitGroup.Done()
	}

	waitGroup.Add(len(materials))
	for _, filePath := range materials {
		go asyncLoadMaterial(filePath)
	}
	waitGroup.Wait()

	return dictionary
}

// generateBspFace Create primitives from face data in the bsp
func generateBspFace(f *face.Face, bspStructure *bspstructs, bspMesh *graphics.BasicMesh) BspFace {
	offset := int32(len(bspMesh.Vertices())) / 3
	length := int32(0)

	planeNormal := bspStructure.planes[f.Planenum].Normal
	// All surfedges associated with this face
	// surfEdges are basically indices into the edges lump
	faceSurfEdges := bspStructure.surfEdges[f.FirstEdge:(f.FirstEdge + int32(f.NumEdges))]
	rootIndex := uint16(0)
	for idx, surfEdge := range faceSurfEdges {
		edge := bspStructure.edges[int(math.Abs(float64(surfEdge)))]
		e1 := 0
		e2 := 1
		if surfEdge < 0 {
			e1 = 1
			e2 = 0
		}
		//Capture root indice
		if idx == 0 {
			rootIndex = edge[e1]
		} else {
			// Just create a triangle for every edge now
			bspMesh.AddIndice(uint32(len(bspMesh.Vertices())) / 3)
			bspMesh.AddVertex(bspStructure.vertexes[rootIndex].X(), bspStructure.vertexes[rootIndex].Y(), bspStructure.vertexes[rootIndex].Z())
			bspMesh.AddNormal(planeNormal.X(), planeNormal.Y(), planeNormal.Z())

			bspMesh.AddIndice(uint32(len(bspMesh.Vertices())) / 3)
			bspMesh.AddVertex(bspStructure.vertexes[edge[e1]].X(), bspStructure.vertexes[edge[e1]].Y(), bspStructure.vertexes[edge[e1]].Z())
			bspMesh.AddNormal(planeNormal.X(), planeNormal.Y(), planeNormal.Z())

			bspMesh.AddIndice(uint32(len(bspMesh.Vertices())) / 3)
			bspMesh.AddVertex(bspStructure.vertexes[edge[e2]].X(), bspStructure.vertexes[edge[e2]].Y(), bspStructure.vertexes[edge[e2]].Z())
			bspMesh.AddNormal(planeNormal.X(), planeNormal.Y(), planeNormal.Z())

			length += 3 // num verts (3 b/c face triangles)
		}
	}

	return NewMeshFace(offset, length, &bspStructure.texInfos[f.TexInfo], f)
}

// generateDisplacementFace Create Primitive from Displacement face
// This is based on:
// https://github.com/Metapyziks/VBspViewer/blob/master/Assets/VBspViewer/Scripts/Importing/VBsp/VBspFile.cs
func generateDisplacementFace(f *face.Face, bspStructure *bspstructs, bspMesh *graphics.BasicMesh) BspFace {
	corners := make([]mgl32.Vec3, 4)
	normal := bspStructure.planes[f.Planenum].Normal

	info := bspStructure.dispInfos[f.DispInfo]
	size := int(1 << uint32(info.Power))
	firstCorner := int32(0)
	firstCornerDist2 := float32(math.MaxFloat32)

	offset := int32(len(bspMesh.Vertices())) / 3
	length := int32(0)

	for surfId := f.FirstEdge; surfId < f.FirstEdge+int32(f.NumEdges); surfId++ {
		surfEdge := bspStructure.surfEdges[surfId]
		edgeIndex := int32(math.Abs(float64(surfEdge)))
		edge := bspStructure.edges[edgeIndex]
		vert := bspStructure.vertexes[edge[0]]
		if surfEdge < 0 {
			vert = bspStructure.vertexes[edge[1]]
		}
		corners[surfId-f.FirstEdge] = vert

		dist2tmp := info.StartPosition.Sub(vert)
		dist2 := (dist2tmp.X() * dist2tmp.X()) + (dist2tmp.Y() * dist2tmp.Y()) + (dist2tmp.Z() * dist2tmp.Z())
		if dist2 < firstCornerDist2 {
			firstCorner = surfId - f.FirstEdge
			firstCornerDist2 = dist2
		}
	}

	for x := 0; x < size; x++ {
		for y := 0; y < size; y++ {
			a := generateDispVert(int(info.DispVertStart), x, y, size, corners, firstCorner, &bspStructure.dispVerts)
			b := generateDispVert(int(info.DispVertStart), x, y+1, size, corners, firstCorner, &bspStructure.dispVerts)
			c := generateDispVert(int(info.DispVertStart), x+1, y+1, size, corners, firstCorner, &bspStructure.dispVerts)
			d := generateDispVert(int(info.DispVertStart), x+1, y, size, corners, firstCorner, &bspStructure.dispVerts)

			// Split into triangles
			bspMesh.AddIndice(uint32(len(bspMesh.Vertices()))/3, (uint32(len(bspMesh.Vertices()))/3)+1, (uint32(len(bspMesh.Vertices()))/3)+2)
			bspMesh.AddVertex(a.X(), a.Y(), a.Z(), b.X(), b.Y(), b.Z(), c.X(), c.Y(), c.Z())
			bspMesh.AddNormal(normal.X(), normal.Y(), normal.Z(), normal.X(), normal.Y(), normal.Z(), normal.X(), normal.Y(), normal.Z())
			bspMesh.AddIndice(uint32(len(bspMesh.Vertices()))/3, (uint32(len(bspMesh.Vertices()))/3)+1, (uint32(len(bspMesh.Vertices()))/3)+2)
			bspMesh.AddVertex(a.X(), a.Y(), a.Z(), c.X(), c.Y(), c.Z(), d.X(), d.Y(), d.Z())
			bspMesh.AddNormal(normal.X(), normal.Y(), normal.Z(), normal.X(), normal.Y(), normal.Z(), normal.X(), normal.Y(), normal.Z())

			length += 6 // 6 b/c quad = 2*triangle
		}
	}

	return NewMeshFace(offset, length, &bspStructure.texInfos[f.TexInfo], f)
}

// generateDispVert Create a displacement vertex
func generateDispVert(offset int, x int, y int, size int, corners []mgl32.Vec3, firstCorner int32, dispVerts *[]dispvert.DispVert) mgl32.Vec3 {
	vert := (*dispVerts)[offset+x+y*(size+1)]

	tx := float32(x) / float32(size)
	ty := float32(y) / float32(size)
	sx := 1.0 - tx
	sy := 1.0 - ty

	cornerA := corners[(0+firstCorner)&3]
	cornerB := corners[(1+firstCorner)&3]
	cornerC := corners[(2+firstCorner)&3]
	cornerD := corners[(3+firstCorner)&3]

	origin := ((cornerB.Mul(sx).Add(cornerC.Mul(tx))).Mul(ty)).Add((cornerA.Mul(sx).Add(cornerD.Mul(tx))).Mul(sy))

	return origin.Add(vert.Vec.Mul(vert.Dist))
}

// TexCoordsForFaceFromTexInfo Generate texturecoordinates for face data
func TexCoordsForFaceFromTexInfo(vertexes []float32, tx *texinfo.TexInfo, width int, height int) []float32 {
	uvs := make([]float32, (len(vertexes)/3)*2)
	for idx := 0; idx < len(vertexes)/3; idx++ {
		//u = tv0,0 * x + tv0,1 * y + tv0,2 * z + tv0,3
		uvs[idx*2] = ((tx.TextureVecsTexelsPerWorldUnits[0][0] * vertexes[(idx*3)]) +
			(tx.TextureVecsTexelsPerWorldUnits[0][1] * vertexes[(idx*3)+1]) +
			(tx.TextureVecsTexelsPerWorldUnits[0][2] * vertexes[(idx*3)+2]) +
			tx.TextureVecsTexelsPerWorldUnits[0][3]) / float32(width)

		//v = tv1,0 * x + tv1,1 * y + tv1,2 * z + tv1,3
		uvs[(idx*2)+1] = ((tx.TextureVecsTexelsPerWorldUnits[1][0] * vertexes[(idx*3)]) +
			(tx.TextureVecsTexelsPerWorldUnits[1][1] * vertexes[(idx*3)+1]) +
			(tx.TextureVecsTexelsPerWorldUnits[1][2] * vertexes[(idx*3)+2]) +
			tx.TextureVecsTexelsPerWorldUnits[1][3]) / float32(height)
	}

	return uvs
}

func GenerateLightmapTexture(faces []face.Face, samples []common.ColorRGBExponent32) *graphics.TextureAtlas {
	lightMapAtlas := graphics.NewTextureAtlas(0, 0)
	textures := make([]*graphics.Texture2D, len(faces))

	for idx, f := range faces {
		textures[idx] = lightmapTextureFromFace(&f, samples)
		lightMapAtlas.AddRaw(textures[idx].Width(), textures[idx].Height(), textures[idx].Image())
	}

	lightMapAtlas.Pack()

	return lightMapAtlas
}

func lightmapTextureFromFace(f *face.Face, samples []common.ColorRGBExponent32) *graphics.Texture2D {
	width := f.LightmapTextureSizeInLuxels[0] + 1
	height := f.LightmapTextureSizeInLuxels[1] + 1
	numLuxels := width * height
	firstSampleIdx := f.Lightofs / 4 // 4= size of ColorRGBExponent32

	raw := make([]uint8, (numLuxels)*3)

	// @TODO This doesnt use the exponent yet
	for idx, sample := range samples[firstSampleIdx : firstSampleIdx+numLuxels] {
		raw[(idx * 3)] = uint8(math.Min(255, float64(sample.R) * math.Pow(2 ,float64(sample.Exponent))))
		raw[(idx*3)+1] = uint8(math.Min(255, float64(sample.G) * math.Pow(2 ,float64(sample.Exponent))))
		raw[(idx*3)+2] = uint8(math.Min(255, float64(sample.B) * math.Pow(2 ,float64(sample.Exponent))))
	}

	return graphics.NewTexture("__lightmap_subtex__", int(width), int(height), uint32(format.RGB888), raw)
}

// LightmapCoordsForFaceFromTexInfo create lightmap coordinates from TexInfo
func LightmapCoordsForFaceFromTexInfo(vertexes []float32,
	faceInfo *face.Face,
	tx *texinfo.TexInfo,
	lightmapWidth float32,
	lightmapHeight float32,
	lightmapOffsetX float32,
	lightmapOffsetY float32) []float32 {
	//vert.lightCoord[0] = DotProduct (vec, MSurf_TexInfo( surfID )->lightmapVecsLuxelsPerWorldUnits[0].AsVector3D()) +
	//	MSurf_TexInfo( surfID )->lightmapVecsLuxelsPerWorldUnits[0][3];
	//vert.lightCoord[0] -= MSurf_LightmapMins( surfID )[0];
	//vert.lightCoord[0] += 0.5f;
	//vert.lightCoord[0] /= ( float )MSurf_LightmapExtents( surfID )[0]; //pSurf->texinfo->texture->width;
	//
	//vert.lightCoord[1] = DotProduct (vec, MSurf_TexInfo( surfID )->lightmapVecsLuxelsPerWorldUnits[1].AsVector3D()) +
	//	MSurf_TexInfo( surfID )->lightmapVecsLuxelsPerWorldUnits[1][3];
	//vert.lightCoord[1] -= MSurf_LightmapMins( surfID )[1];
	//vert.lightCoord[1] += 0.5f;
	//vert.lightCoord[1] /= ( float )MSurf_LightmapExtents( surfID )[1]; //pSurf->texinfo->texture->height;
	//
	//vert.lightCoord[0] = sOffset + vert.lightCoord[0] * sScale;
	//vert.lightCoord[1] = tOffset + vert.lightCoord[1] * tScale;

	uvs := make([]float32, (len(vertexes)/3)*2)

	sScale := 1 / lightmapWidth
	sOffset := lightmapOffsetX * sScale
	sScale = float32(faceInfo.LightmapTextureSizeInLuxels[0]) * sScale


	tScale := 1 / lightmapHeight
	tOffset := lightmapOffsetY * tScale
	tScale = float32(faceInfo.LightmapTextureSizeInLuxels[1]) * tScale

	// 0x00000001 = SURFDRAW_NOLIGHT
	if tx.Flags & 0x00000001 != 0 {
		for idx := 0; idx < len(vertexes)/3; idx++ {
			uvs[(idx * 2) + 0] = 0.5
			uvs[(idx * 2) + 1] = 0.5
		}
		return uvs
	}

	if faceInfo.LightmapTextureSizeInLuxels[0] == 0 {
		for idx := 0; idx < len(vertexes)/3; idx++ {
			uvs[(idx * 2) + 0] = sOffset
			uvs[(idx * 2) + 1] = tOffset
		}
		return uvs
	}

	for idx := 0; idx < len(vertexes)/3; idx++ {
		uvs[(idx * 2) + 0] =
			(mgl32.Vec3{vertexes[(idx * 3) + 0], vertexes[(idx * 3) + 1], vertexes[(idx * 3) + 2]}).Dot(
			mgl32.Vec3{tx.LightmapVecsLuxelsPerWorldUnits[0][0], tx.LightmapVecsLuxelsPerWorldUnits[0][1],tx.LightmapVecsLuxelsPerWorldUnits[0][2]}) +
				tx.LightmapVecsLuxelsPerWorldUnits[0][3]
		uvs[(idx * 2) + 0] -= float32(faceInfo.LightmapTextureMinsInLuxels[0])
		uvs[(idx * 2) + 0] += 0.5
		uvs[(idx * 2) + 0] /= float32(faceInfo.LightmapTextureSizeInLuxels[0])

		uvs[(idx * 2) + 1] =
			(mgl32.Vec3{vertexes[(idx * 3) + 0], vertexes[(idx * 3) + 1], vertexes[(idx * 3) + 2]}).Dot(
				mgl32.Vec3{tx.LightmapVecsLuxelsPerWorldUnits[1][0], tx.LightmapVecsLuxelsPerWorldUnits[1][1],tx.LightmapVecsLuxelsPerWorldUnits[1][2]}) +
				tx.LightmapVecsLuxelsPerWorldUnits[1][3]
		uvs[(idx * 2) + 1] -= float32(faceInfo.LightmapTextureMinsInLuxels[1])
		uvs[(idx * 2) + 1] += 0.5
		uvs[(idx * 2) + 1] /= float32(faceInfo.LightmapTextureSizeInLuxels[1])


		uvs[(idx * 2) + 0] = sOffset + uvs[(idx * 2) + 0] * sScale
		uvs[(idx * 2) + 1] = tOffset + uvs[(idx * 2) + 1] * tScale
	}

	return uvs
}

// Bsp
type Bsp struct {
	file *bsp.Bsp

	mesh      *graphics.BasicMesh
	faces     []BspFace
	dispFaces []int

	materialDictionary map[string]*graphics.Material
	textureInfos       []texinfo.TexInfo

	StaticPropDictionary map[string]*graphics.Model
	StaticProps          []graphics.StaticProp

	camera *graphics3d.Camera

	lightmapAtlas *graphics.TextureAtlas
}

// BasicMesh
func (bsp *Bsp) Mesh() *graphics.BasicMesh {
	return bsp.mesh
}

// Faces
func (bsp *Bsp) Faces() []BspFace {
	return bsp.faces
}

// DispFaces
func (bsp *Bsp) DispFaces() []int {
	return bsp.dispFaces
}

// MaterialDictionary
func (bsp *Bsp) MaterialDictionary() map[string]*graphics.Material {
	return bsp.materialDictionary
}

func (bsp *Bsp) TexInfos() []texinfo.TexInfo {
	return bsp.textureInfos
}

func (bsp *Bsp) Camera() *graphics3d.Camera {
	return bsp.camera
}

func (bsp *Bsp) SetCamera(camera *graphics3d.Camera) {
	bsp.camera = camera
}

func (bsp *Bsp) File() *bsp.Bsp {
	return bsp.file
}

func (bsp *Bsp) LightmapAtlas() *graphics.TextureAtlas {
	return bsp.lightmapAtlas
}

// NewBsp
func NewBsp(
	file *bsp.Bsp,
	mesh *graphics.BasicMesh,
	faces []BspFace,
	dispFaces []int,
	materialDictionary map[string]*graphics.Material,
	textureInfos []texinfo.TexInfo,
	lightmapAtlas *graphics.TextureAtlas) *Bsp {
	return &Bsp{
		file:               file,
		mesh:               mesh,
		faces:              faces,
		dispFaces:          dispFaces,
		materialDictionary: materialDictionary,
		textureInfos:       textureInfos,
		lightmapAtlas:      lightmapAtlas,
	}
}

// BspFace
type BspFace struct {
	offset   int
	length   int
	material string
	texInfo  *texinfo.TexInfo
	bspFace  *face.Face
}

// Offset
func (face *BspFace) Offset() int {
	return face.offset
}

// Length
func (face *BspFace) Length() int {
	return face.length
}

func (face *BspFace) Material() string {
	return face.material
}

func (face *BspFace) SetMaterial(materialPath string) {
	face.material = materialPath
}

func (face *BspFace) TexInfo() *texinfo.TexInfo {
	return face.texInfo
}

func (face *BspFace) RawFace() *face.Face {
	return face.bspFace
}

// NewFace
func NewMeshFace(offset int32, length int32, texInfo *texinfo.TexInfo, bspFace *face.Face) BspFace {
	return BspFace{
		offset:  int(offset),
		length:  int(length),
		texInfo: texInfo,
		bspFace: bspFace,
	}
}
