package viewer

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func benchmarkArchive(b *testing.B) (*App, Camera) {
	b.Helper()
	dir := b.TempDir()
	store, err := OpenStore(filepath.Join(dir, "data"))
	if err != nil { b.Fatal(err) }
	b.Cleanup(func(){store.Close()})
	c := Camera{ID:"synthetic",Name:"Synthetic",Folder:"camera",Timezone:"Europe/Berlin",Format:"automatic",IndexingMode:"full"}
	if err:=store.SaveCamera(c);err!=nil { b.Fatal(err) }
	tx,err:=store.Begin()
	if err!=nil { b.Fatal(err) }
	for i:=0;i<10000;i++ {
		name:=fmt.Sprintf("camera/%05d.mp4",i)
		_,err=tx.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,?,?,?,?,?)`,fmt.Sprintf("%05d",i),c.ID,name,1791950400+float64(i*60),30,"h264",1000,0,filepath.Base(name))
		if err!=nil { b.Fatal(err) }
	}
	if err:=tx.Commit();err!=nil { b.Fatal(err) }
	rootDir:=filepath.Join(dir,"recordings")
	if err:=os.MkdirAll(filepath.Join(rootDir,"camera"),0700);err!=nil { b.Fatal(err) }
	root,err:=os.OpenRoot(rootDir)
	if err!=nil { b.Fatal(err) }
	b.Cleanup(func(){root.Close()})
	return &App{db:store,root:root,ctx:context.Background()},c
}

func BenchmarkDayRecordings(b *testing.B) {
	a,c:=benchmarkArchive(b)
	b.ResetTimer()
	for i:=0;i<b.N;i++ { if _,err:=a.db.DayRecordings([]Camera{c},1791950400,1792036800);err!=nil { b.Fatal(err) } }
}

func BenchmarkSearchRecordings(b *testing.B) {
	a,_:=benchmarkArchive(b)
	b.ResetTimer()
	for i:=0;i<b.N;i++ {
		r:=httptest.NewRequest("GET","/api/search?camera=synthetic&from=1791950400",nil)
		w:=httptest.NewRecorder()
		a.searchRecordings(w,r)
		if w.Code!=200 { b.Fatal(w.Code,w.Body.String()) }
	}
}

func BenchmarkWalkMedia(b *testing.B) {
	dir:=b.TempDir()
	if err:=os.MkdirAll(filepath.Join(dir,"camera"),0700);err!=nil { b.Fatal(err) }
	for i:=0;i<1000;i++ { if err:=os.WriteFile(filepath.Join(dir,"camera",fmt.Sprintf("%05d.mp4",i)),nil,0600);err!=nil { b.Fatal(err) } }
	root,err:=os.OpenRoot(dir)
	if err!=nil { b.Fatal(err) }
	defer root.Close()
	a:=&App{root:root,ctx:context.Background()}
	b.ResetTimer()
	for i:=0;i<b.N;i++ { if err:=a.walkMedia("camera",0,func(string,os.DirEntry)error{return nil});err!=nil { b.Fatal(err) } }
}
