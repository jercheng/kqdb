package filem

import (
	"container/list"
	"encoding/binary"
	"io/ioutil"
	"kqdb/src/global"
	"log"
	"os"
	"path/filepath"
	"strings"
)

//文件管理模块 fileName带后缀
func init() {
	global.InitLog()
}

const (
	SIZE_B              int = 1
	SIZE_K                  = 1024 * SIZE_B
	SIZE_M                  = 1024 * SIZE_K
	SIZE_G                  = 1024 * SIZE_M
	DATA_FILE_INIT_SIZE     = 9 * SIZE_M //数据文件初始大小
	PageSize                = 8 * SIZE_K //分页大小
	DataFileSuf             = "data"     //数据文件扩展名
	FrameFileSuf            = "frm"      //结构文件扩展名
)

//定义列数据类型枚举值
type FileType int

const (
	FileTypeData FileType = 1 + iota
	FileTypeFrame
)

//===========================

var filesMap = initFilesMap()

//key为schema和table。0位置为frm文件，1位置为data文件
func initFilesMap() map[string]map[string][2]*FileHandler {
	filesMap := make(map[string]map[string][2]*FileHandler)

	//获取所有schema
	dirNames := ListDir(global.DataDir)

	for _, dirName := range dirNames {
		schemaName := dirName

		dirPath := filepath.Join(global.DataDir, dirName)
		fileNames := ListFile(dirPath, FrameFileSuf)

		fileMap := make(map[string][2]*FileHandler)
		for _, fileName := range fileNames {
			tableName := strings.TrimSuffix(fileName, "."+FrameFileSuf)

			frameFileHandler := openFile(FileTypeFrame, schemaName, tableName)
			dataFileHandler := openFile(FileTypeData, schemaName, tableName)

			fileMap[tableName] = [2]*FileHandler{frameFileHandler, dataFileHandler}
		}

		filesMap[schemaName] = fileMap
	}

	return filesMap
}

func CloseFilesMap() {
	for schemaName := range filesMap {
		fileMap := filesMap[schemaName]
		for tableName := range fileMap {
			files := fileMap[tableName]
			files[0].Close()
			files[1].Close()
		}
	}
}

//获取指定目录下的所有目录
func ListDir(dirPth string) (dirNames []string) {
	dirNames = make([]string, 0, 10)
	dir, err := ioutil.ReadDir(dirPth)
	if err != nil {
		log.Panic(err)
	}
	for _, fi := range dir {
		if fi.IsDir() {
			dirNames = append(dirNames, fi.Name())
		} else {
			continue
		}
	}
	return
}

//获取指定目录下的所有文件，不进入下一级目录搜索，可以匹配后缀过滤。
func ListFile(dirPth string, suffix string) (fileNames []string) {
	fileNames = make([]string, 0, 10)
	dir, err := ioutil.ReadDir(dirPth)
	if err != nil {
		log.Panic(err)
	}
	suffix = strings.ToUpper(suffix) //忽略后缀匹配的大小写
	for _, fi := range dir {
		if fi.IsDir() { // 忽略目录
			continue
		}
		if strings.HasSuffix(strings.ToUpper(fi.Name()), "."+suffix) { //匹配文件
			fileNames = append(fileNames, fi.Name())
		}
	}
	return
}

//===========================

type FileHandler struct {
	fileType  FileType
	Path      string //相对于data文件夹
	FileName  string
	File      *os.File
	TotalPage int
}

func CreateDataFile(schemaName string, tableName string) {
	log.Printf("开始创建数据文件:%s.%s\n", tableName, DataFileSuf)

	dataPath := filepath.Join(global.DataDir, schemaName, tableName+"."+DataFileSuf)
	file, err := os.Create(dataPath)
	defer file.Close()
	if err != nil {
		log.Panicln("创建数据文件:%s.%s失败\n", tableName, DataFileSuf)
	}

	//文件头page
	metabytes := make([]byte, PageSize)
	file.Write(metabytes)

	//初始化data-page
	pageBuf := make([]byte, PageSize)
	pageLower := uint16(24)
	pageUpper := uint16(PageSize - 1)
	binary.BigEndian.PutUint16(pageBuf[2:4], pageLower)
	binary.BigEndian.PutUint16(pageBuf[4:6], pageUpper)

	//写入初始化data-page
	num := DATA_FILE_INIT_SIZE/PageSize - 1
	for i := 0; i < int(num); i++ {
		file.Write(pageBuf)
	}

	//FilesMap处理
	frameFileHandler := openFile(FileTypeFrame, schemaName, tableName)
	dataFileHandler := openFile(FileTypeData, schemaName, tableName)
	filesMap[schemaName][tableName] = [2]*FileHandler{frameFileHandler, dataFileHandler}

	//buffer_pool处理
	pageList := list.New()
	for i := 1; i < 10; i++ {
		page := dataFileHandler.GetPage(i)
		pageList.PushBack(page)
	}
	bufferTable := bufferTable{pageList, list.New()}
	bufferPool[schemaName][tName(tableName)] = &bufferTable

	log.Printf("创建数据文件:%s.%s.%s成功\n", schemaName, tableName, DataFileSuf)
}

func openFile(fileType FileType, schemaName string, tableName string) (fileHandler *FileHandler) {
	suf := ""
	switch fileType {
	case FileTypeData:
		suf = DataFileSuf
	case FileTypeFrame:
		suf = FrameFileSuf
	default:
		log.Panicln("不支持的文件类型:", fileType)
	}

	address := filepath.Join(global.DataDir, schemaName, tableName+"."+suf)
	datafile, err := os.OpenFile(address, os.O_RDWR, os.ModePerm)
	if err == nil {
		fileHandler = new(FileHandler)
		fileHandler.Path = schemaName
		fileHandler.FileName = tableName
		fileHandler.fileType = fileType
		fileHandler.File = datafile

		if fileType == FileTypeData {
			fileInfo, err := datafile.Stat()
			if err != nil {
				log.Panicln(err)
			}
			if int(fileInfo.Size())%PageSize != 0 {
				log.Panicln("数据文件长度出错")
			}
			fileHandler.TotalPage = int(fileInfo.Size()) / PageSize
		}

	} else {
		log.Panicln(err)
	}
	log.Printf("打开文件：%s/%s.%s\n", fileHandler.Path, fileHandler.FileName, suf)
	return
}

func GetFile(fileType FileType, schemaName string, tableName string) (fileHandler *FileHandler) {
	switch fileType {
	case FileTypeData:
		fileHandler = filesMap[schemaName][string(tableName)][1]
	case FileTypeFrame:
		fileHandler = filesMap[schemaName][string(tableName)][0]
	default:
		log.Panicln("不支持的文件类型:", fileType)
	}
	return
}

func (fh *FileHandler) Close() error {
	err := fh.File.Close()
	suf := ""
	switch fh.fileType {
	case FileTypeData:
		suf = DataFileSuf
	case FileTypeFrame:
		suf = FrameFileSuf
	default:
		log.Panicln("不支持的文件类型:", fh.fileType)
	}
	log.Printf("关闭文件：%s/%s.%s\n", fh.Path, fh.FileName, suf)
	return err
}

//先从bufferPool获取，然后在从硬盘中取
func (fh *FileHandler) GetPage(pageNum int) *Page {
	if fh.fileType != FileTypeData {
		log.Panic("不是数据文件")
	}

	//从buffer_pool中获取page
	tName := tName(fh.FileName)
	var page *Page
	pageList := bufferPool[fh.Path][tName].PageList
	for e := pageList.Front(); e != nil; e = e.Next() {
		p := e.Value.(*Page)
		if p.PageNum == pageNum {
			page = p
			break
		}
	}

	//如果page链上没有，从脏链上获取
	if page == nil {
		dirtyPageList := bufferPool[fh.Path][tName].DirtyPageList
		for e := dirtyPageList.Front(); e != nil; e = e.Next() {
			p := e.Value.(*Page)
			if p.PageNum == pageNum {
				page = p
				break
			}
		}
	}

	//如果buffer_pool中page不存在，从文件中获取page，并放入buffer_pool
	if page == nil {
		page = fh.getPageFromDisk(pageNum)
		//放入buffer_pool
		pageList.PushBack(page)
	}

	return page
}

//从硬盘获取page
func (fh *FileHandler) getPageFromDisk(pageNum int) *Page {
	if fh.fileType != FileTypeData {
		log.Panic("不是数据文件")
	}

	bytes := make([]byte, PageSize)
	offset := pageNum * PageSize
	_, err := fh.File.ReadAt(bytes, int64(offset))
	if err != nil {
		log.Panic(err)
	}
	page := new(Page)
	page.UnMarshal(bytes, pageNum, fh.Path, fh.FileName)
	return page
}

func (fh *FileHandler) MarkDirty(pageNum int) {
	t := tName(fh.FileName)
	dirtyPageList := bufferPool[fh.Path][t].DirtyPageList
	pageList := bufferPool[fh.Path][t].PageList

	var resultPage *Page = nil

	for e := dirtyPageList.Front(); e != nil; e = e.Next() {
		dirtyPage := e.Value.(*Page)
		if pageNum == dirtyPage.PageNum {
			resultPage = dirtyPage
			break
		}
	}

	//当dirty链上不存在时，从page链上查找
	if resultPage == nil {
		for e := pageList.Front(); e != nil; e = e.Next() {
			page := e.Value.(*Page)
			if pageNum == page.PageNum {
				resultPage = page
				//从page链中删除当前page
				pageList.Remove(e)
				break
			}

		}
	}

	if resultPage != nil {
		//加入dirty链
		dirtyPageList.PushBack(resultPage)
	} else {
		log.Panicln("bufferPool不存在page：", pageNum)
	}

}
