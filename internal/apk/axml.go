package apk

import (
	"bytes"
	"encoding/binary"
	"sort"
	"unicode/utf16"
)

// Binary AndroidManifest.xml encoder (AXML). Format: frameworks/base ResourceTypes.h.
// Lets -package / -target-sdk / etc. change without aapt2.

const (
	chunkXML         = 0x0003
	chunkStringPool  = 0x0001
	chunkResourceMap = 0x0180
	chunkStartNS     = 0x0100
	chunkEndNS       = 0x0101
	chunkStartElem   = 0x0102
	chunkEndElem     = 0x0103

	typeString     = 0x03
	typeIntDec     = 0x10
	typeReference  = 0x01
	typeIntBoolean = 0x12

	boolTrue  = 0xFFFFFFFF
	noRef     = 0xFFFFFFFF
	androidNS = "http://schemas.android.com/apk/res/android"

	// @android:style/Theme.NoTitleBar.Fullscreen
	themeNoTitleFullscreen = 0x01030007
)

// Framework attribute resource IDs.
var attrResID = map[string]uint32{
	"label":                0x01010001,
	"name":                 0x01010003,
	"theme":                0x01010000,
	"hasCode":              0x0101000c,
	"exported":             0x01010010,
	"minSdkVersion":        0x0101020c,
	"versionCode":          0x0101021b,
	"versionName":          0x0101021c,
	"targetSdkVersion":     0x01010270,
	"extractNativeLibs":    0x010104ea,
	"usesCleartextTraffic": 0x010104ec,
}

type ManifestParams struct {
	Package       string
	Label         string
	VersionCode   int
	VersionName   string
	MinSDK        int
	TargetSDK     int
	ActivityClass string
	// IconRef, if set (e.g. "@mipmap/ic_launcher"), adds android:icon on
	// <application>. Used when generating XML for aapt2; the binary
	// EncodeManifest path does not resolve app resource refs.
	IconRef string
}

type attr struct {
	name  string
	ns    bool // android: namespace when true
	typ   uint8
	data  uint32
	str   string
	isStr bool
}

type elem struct {
	name     string
	attrs    []attr
	children []*elem
}

// Attr names with resource IDs must come first in the string pool (resource map order).
type strPool struct {
	list   []string
	index  map[string]int
	resIDs []uint32
}

func newStrPool() *strPool {
	return &strPool{index: map[string]int{}}
}

func (p *strPool) addAttrName(name string, resID uint32) {
	p.index[name] = len(p.list)
	p.list = append(p.list, name)
	p.resIDs = append(p.resIDs, resID)
}

func (p *strPool) get(s string) uint32 {
	if i, ok := p.index[s]; ok {
		return uint32(i)
	}
	i := len(p.list)
	p.index[s] = i
	p.list = append(p.list, s)
	return uint32(i)
}

func b2u(b bool) uint32 {
	if b {
		return boolTrue
	}
	return 0
}

func buildManifestTree(p ManifestParams) *elem {
	intAttr := func(name string, v int) attr {
		return attr{name: name, ns: true, typ: typeIntDec, data: uint32(v)}
	}
	strAttr := func(name, v string) attr {
		return attr{name: name, ns: true, typ: typeString, str: v, isStr: true}
	}
	boolAttr := func(name string, v bool) attr {
		return attr{name: name, ns: true, typ: typeIntBoolean, data: b2u(v)}
	}
	refAttr := func(name string, id uint32) attr {
		return attr{name: name, ns: true, typ: typeReference, data: id}
	}

	return &elem{
		name: "manifest",
		attrs: []attr{
			intAttr("versionCode", p.VersionCode),
			strAttr("versionName", p.VersionName),
			{name: "package", ns: false, typ: typeString, str: p.Package, isStr: true},
		},
		children: []*elem{
			{name: "uses-sdk", attrs: []attr{
				intAttr("minSdkVersion", p.MinSDK),
				intAttr("targetSdkVersion", p.TargetSDK),
			}},
			{name: "uses-permission", attrs: []attr{
				strAttr("name", "android.permission.INTERNET"),
			}},
			{name: "uses-permission", attrs: []attr{
				strAttr("name", "android.permission.POST_NOTIFICATIONS"),
			}},
			{name: "application", attrs: applicationAttrs(p, strAttr, boolAttr), children: []*elem{
				{name: "activity", attrs: []attr{
					strAttr("name", p.ActivityClass),
					refAttr("theme", themeNoTitleFullscreen),
					boolAttr("exported", true),
				}, children: []*elem{
					{name: "intent-filter", children: []*elem{
						{name: "action", attrs: []attr{
							strAttr("name", "android.intent.action.MAIN"),
						}},
						{name: "category", attrs: []attr{
							strAttr("name", "android.intent.category.LAUNCHER"),
						}},
					}},
				}},
			}},
		},
	}
}

func applicationAttrs(p ManifestParams, strAttr func(string, string) attr, boolAttr func(string, bool) attr) []attr {
	attrs := []attr{strAttr("label", p.Label)}
	if p.IconRef != "" {
		attrs = append(attrs, strAttr("icon", p.IconRef))
	}
	return append(attrs,
		boolAttr("hasCode", true),
		boolAttr("extractNativeLibs", true),
		boolAttr("usesCleartextTraffic", true),
	)
}

func EncodeManifest(p ManifestParams) []byte {
	if p.ActivityClass == "" {
		p.ActivityClass = "com.gofront.app.MainActivity"
	}
	root := buildManifestTree(p)

	pool := newStrPool()
	type ar struct {
		name string
		id   uint32
	}
	names := make([]ar, 0, len(attrResID))
	for n, id := range attrResID {
		names = append(names, ar{n, id})
	}
	sort.Slice(names, func(i, j int) bool { return names[i].id < names[j].id })
	for _, a := range names {
		pool.addAttrName(a.name, a.id)
	}

	prefixIdx := pool.get("android")
	uriIdx := pool.get(androidNS)

	var nodes bytes.Buffer
	writeStartNamespace(&nodes, prefixIdx, uriIdx)
	writeElem(&nodes, pool, root, uriIdx)
	writeEndNamespace(&nodes, prefixIdx, uriIdx)

	poolBytes := encodeStringPool(pool)
	resMap := encodeResourceMap(pool.resIDs)

	total := 8 + len(poolBytes) + len(resMap) + nodes.Len()
	var out bytes.Buffer
	writeChunkHeader(&out, chunkXML, 8, uint32(total))
	out.Write(poolBytes)
	out.Write(resMap)
	out.Write(nodes.Bytes())
	return out.Bytes()
}

func writeElem(buf *bytes.Buffer, pool *strPool, e *elem, uriIdx uint32) {
	attrs := append([]attr(nil), e.attrs...)
	sort.SliceStable(attrs, func(i, j int) bool {
		ri, oki := attrResID[attrs[i].name]
		rj, okj := attrResID[attrs[j].name]
		if oki && okj {
			return ri < rj
		}
		return oki && !okj
	})

	nameIdx := pool.get(e.name)

	body := new(bytes.Buffer)
	binary.Write(body, binary.LittleEndian, uint32(noRef))
	binary.Write(body, binary.LittleEndian, nameIdx)
	binary.Write(body, binary.LittleEndian, uint16(20)) // attributeStart
	binary.Write(body, binary.LittleEndian, uint16(20)) // attributeSize
	binary.Write(body, binary.LittleEndian, uint16(len(attrs)))
	binary.Write(body, binary.LittleEndian, uint16(0)) // idIndex
	binary.Write(body, binary.LittleEndian, uint16(0)) // classIndex
	binary.Write(body, binary.LittleEndian, uint16(0)) // styleIndex

	for _, a := range attrs {
		var ns uint32 = noRef
		if a.ns {
			ns = uriIdx
		}
		nIdx := pool.get(a.name)
		var raw uint32 = noRef
		data := a.data
		if a.isStr {
			s := pool.get(a.str)
			raw = s
			data = s
		}
		binary.Write(body, binary.LittleEndian, ns)
		binary.Write(body, binary.LittleEndian, nIdx)
		binary.Write(body, binary.LittleEndian, raw)
		binary.Write(body, binary.LittleEndian, uint16(8)) // value size
		body.WriteByte(0)                                  // res0
		body.WriteByte(a.typ)
		binary.Write(body, binary.LittleEndian, data)
	}

	writeNode(buf, chunkStartElem, body.Bytes())

	for _, c := range e.children {
		writeElem(buf, pool, c, uriIdx)
	}

	end := new(bytes.Buffer)
	binary.Write(end, binary.LittleEndian, uint32(noRef))
	binary.Write(end, binary.LittleEndian, nameIdx)
	writeNode(buf, chunkEndElem, end.Bytes())
}

func writeStartNamespace(buf *bytes.Buffer, prefix, uri uint32) {
	body := new(bytes.Buffer)
	binary.Write(body, binary.LittleEndian, prefix)
	binary.Write(body, binary.LittleEndian, uri)
	writeNode(buf, chunkStartNS, body.Bytes())
}

func writeEndNamespace(buf *bytes.Buffer, prefix, uri uint32) {
	body := new(bytes.Buffer)
	binary.Write(body, binary.LittleEndian, prefix)
	binary.Write(body, binary.LittleEndian, uri)
	writeNode(buf, chunkEndNS, body.Bytes())
}

// ResXMLTree_node: 16-byte header (incl. lineNumber + comment) then body.
func writeNode(buf *bytes.Buffer, typ uint16, body []byte) {
	size := 16 + len(body)
	binary.Write(buf, binary.LittleEndian, typ)
	binary.Write(buf, binary.LittleEndian, uint16(16)) // headerSize
	binary.Write(buf, binary.LittleEndian, uint32(size))
	binary.Write(buf, binary.LittleEndian, uint32(0))     // lineNumber
	binary.Write(buf, binary.LittleEndian, uint32(noRef)) // comment
	buf.Write(body)
}

func writeChunkHeader(buf *bytes.Buffer, typ, headerSize uint16, size uint32) {
	binary.Write(buf, binary.LittleEndian, typ)
	binary.Write(buf, binary.LittleEndian, headerSize)
	binary.Write(buf, binary.LittleEndian, size)
}

func encodeResourceMap(ids []uint32) []byte {
	var buf bytes.Buffer
	size := 8 + 4*len(ids)
	writeChunkHeader(&buf, chunkResourceMap, 8, uint32(size))
	for _, id := range ids {
		binary.Write(&buf, binary.LittleEndian, id)
	}
	return buf.Bytes()
}

func encodeStringPool(pool *strPool) []byte {
	var data bytes.Buffer
	offsets := make([]uint32, len(pool.list))
	for i, s := range pool.list {
		offsets[i] = uint32(data.Len())
		u := utf16.Encode([]rune(s))
		binary.Write(&data, binary.LittleEndian, uint16(len(u)))
		for _, c := range u {
			binary.Write(&data, binary.LittleEndian, c)
		}
		binary.Write(&data, binary.LittleEndian, uint16(0)) // null terminator
	}
	for data.Len()%4 != 0 {
		data.WriteByte(0)
	}

	stringsStart := 28 + 4*len(offsets)
	size := stringsStart + data.Len()

	var buf bytes.Buffer
	writeChunkHeader(&buf, chunkStringPool, 28, uint32(size))
	binary.Write(&buf, binary.LittleEndian, uint32(len(offsets))) // stringCount
	binary.Write(&buf, binary.LittleEndian, uint32(0))            // styleCount
	binary.Write(&buf, binary.LittleEndian, uint32(0))            // flags (UTF-16)
	binary.Write(&buf, binary.LittleEndian, uint32(stringsStart)) // stringsStart
	binary.Write(&buf, binary.LittleEndian, uint32(0))            // stylesStart
	for _, o := range offsets {
		binary.Write(&buf, binary.LittleEndian, o)
	}
	buf.Write(data.Bytes())
	return buf.Bytes()
}
