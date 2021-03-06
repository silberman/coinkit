package data

import (
	"log"
	"os"
	"testing"

	"github.com/lacker/coinkit/currency"
)

func TestInsertAndGet(t *testing.T) {
	db := NewTestDatabase(0)
	block := &Block{
		Slot:  3,
		Chunk: currency.NewEmptyChunk(),
	}
	err := db.InsertBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	b2 := db.GetBlock(3)
	if b2.C != block.C {
		t.Fatal("block changed: %+v -> %+v", block, b2)
	}
}

func TestGetNonexistentBlock(t *testing.T) {
	db := NewTestDatabase(0)
	b := db.GetBlock(4)
	if b != nil {
		t.Fatal("block should be nonexistent")
	}
}

func TestCantInsertTwice(t *testing.T) {
	db := NewTestDatabase(0)
	block := &Block{
		Slot:  4,
		Chunk: currency.NewEmptyChunk(),
		C:     1,
		H:     2,
	}
	err := db.InsertBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	err = db.InsertBlock(block)
	if err == nil {
		t.Fatal("a block should not save twice")
	}
}

func TestLastBlock(t *testing.T) {
	DropTestData(0)
	db := NewTestDatabase(0)
	b := db.LastBlock()
	if b != nil {
		t.Fatal("expected last block nil but got %+v", b)
	}
	b = &Block{
		Slot:  5,
		Chunk: currency.NewEmptyChunk(),
	}
	err := db.InsertBlock(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Slot = 6
	err = db.InsertBlock(b)
	if err != nil {
		t.Fatal(err)
	}
	b2 := db.LastBlock()
	if b2.Slot != b.Slot {
		t.Fatal("b2: %+v", b2)
	}
}

func TestForBlocks(t *testing.T) {
	DropTestData(0)
	db := NewTestDatabase(0)
	for i := 1; i <= 5; i++ {
		b := &Block{
			Slot:  i,
			Chunk: currency.NewEmptyChunk(),
			C:     7,
		}
		if db.InsertBlock(b) != nil {
			t.Fatal("block could not save")
		}
	}
	count := db.ForBlocks(func(b *Block) {
		if b.C != 7 {
			t.Fatal("expected C = 7")
		}
	})
	if count != 5 {
		t.Fatal("expected count = 5")
	}
}

func TestTotalSizeInfo(t *testing.T) {
	DropTestData(0)
	db := NewTestDatabase(0)
	b := &Block{
		Slot:  1,
		Chunk: currency.NewEmptyChunk(),
		C:     8,
	}
	err := db.InsertBlock(b)
	if err != nil {
		t.Fatalf("could not save. got error: %s", err)
	}
	log.Print(db.TotalSizeInfo())
}

func TestGetDocuments(t *testing.T) {
	DropTestData(0)
	db := NewTestDatabase(0)
	for a := 1; a <= 2; a++ {
		for b := 1; b <= 2; b++ {
			d := NewDocument(uint64(10*a+b), map[string]interface{}{
				"a": a,
				"b": b,
			})
			err := db.InsertDocument(d)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	docs := db.GetDocuments(map[string]interface{}{"a": 2, "b": 1}, 2)
	if len(docs) != 1 {
		t.Fatalf("expected one doc but got: %+v", docs)
	}
}

func TestGetDocumentsNoResults(t *testing.T) {
	DropTestData(0)
	db := NewTestDatabase(0)
	docs := db.GetDocuments(map[string]interface{}{"blorp": "hi"}, 3)
	if len(docs) != 0 {
		t.Fatalf("expected zero docs but got: %+v", docs)
	}
}

const benchmarkMax = 400

func databaseForBenchmarking() *Database {
	DropTestData(0)
	db := NewTestDatabase(0)
	log.Printf("populating db for benchmarking")
	items := 0
	for a := 0; a < benchmarkMax; a++ {
		if a != 0 && a%10 == 0 {
			log.Printf("inserted %d items", items)
		}
		for b := 0; b < benchmarkMax; b++ {
			c := b*benchmarkMax + a + 1
			d := NewDocument(uint64(c), map[string]interface{}{
				"a": a,
				"b": b,
				"c": c,
			})
			err := db.InsertDocument(d)
			if err != nil {
				log.Fatal(err)
			}
			items++
		}
	}
	log.Printf("database is populated with %d items", items)
	return db
}

func BenchmarkOneConstraint(b *testing.B) {
	db := databaseForBenchmarking()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := i%(benchmarkMax*benchmarkMax) + 1
		docs := db.GetDocuments(map[string]interface{}{"c": c}, 2)
		if len(docs) != 1 {
			log.Fatalf("expected one doc for c = %d but got: %+v", c, docs)
		}
	}
}

func BenchmarkTwoConstraints(b *testing.B) {
	db := databaseForBenchmarking()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := i % benchmarkMax
		b := ((i - a) / benchmarkMax) % benchmarkMax
		docs := db.GetDocuments(map[string]interface{}{"a": a, "b": b}, 2)
		if len(docs) != 1 {
			log.Fatalf("expected one doc but got: %+v", docs)
		}
	}
}

// Clean up both before and after running tests
func TestMain(m *testing.M) {
	DropTestData(0)
	answer := m.Run()
	DropTestData(0)
	os.Exit(answer)
}
