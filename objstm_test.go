package gomupdf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"
)

// brokenObjStmPDF is a one-page PDF with an xref stream whose last n
// entries all lie in one object stream (object 4) whose data does not
// parse. Objects: 1 catalog, 2 pages, 3 page, 4 the object stream, 5 the
// page content, 6 the xref stream, 7.. the n missing objects.
func brokenObjStmPDF(n int) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	content := "BT /F1 24 Tf 20 100 Td (Hello) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> /Contents 5 0 R >>",
		fmt.Sprintf("<< /Type /ObjStm /N %d /First 10 /Length 8 >>\nstream\nnot pdf!\nendstream", n),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	size := len(objs) + 2 + n
	var rows bytes.Buffer
	row := func(typ byte, f2 uint32, f3 uint16) {
		rows.WriteByte(typ)
		binary.Write(&rows, binary.BigEndian, f2)
		binary.Write(&rows, binary.BigEndian, f3)
	}
	row(0, 0, 65535)
	for i := range objs {
		row(1, uint32(offs[i]), 0)
	}
	xref := b.Len()
	row(1, uint32(xref), 0)
	for i := range n {
		row(2, 4, uint16(i))
	}
	fmt.Fprintf(&b, "%d 0 obj\n<< /Type /XRef /Size %d /W [1 4 2] /Root 1 0 R /Length %d >>\nstream\n",
		len(objs)+1, size, rows.Len())
	b.Write(rows.Bytes())
	fmt.Fprintf(&b, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

// Opening loads every object, but an object stream that does not parse is
// tried once, not once per object in it: 100000 objects in one broken
// stream open in well under a second (23s before). The page still reads.
func TestOpenBrokenObjectStream(t *testing.T) {
	pdf := brokenObjStmPDF(100000)
	start := time.Now()
	d, err := OpenStream(pdf)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if took > time.Second {
		t.Errorf("OpenStream took %v", took)
	}
	if n := d.PageCount(); n != 1 {
		t.Fatalf("%d pages, want 1", n)
	}
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if txt, err := p.GetText(); err != nil || !strings.Contains(txt, "Hello") {
		t.Errorf("text = %q, %v", txt, err)
	}
}
