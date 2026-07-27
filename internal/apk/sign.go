package apk

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math/big"
	"strings"
	"time"

	"github.com/agusibrahim/apksig-go/pkg/algo"
	"github.com/agusibrahim/apksig-go/pkg/apksigblock"
	"github.com/agusibrahim/apksig-go/pkg/apkwriter"
	"github.com/agusibrahim/apksig-go/pkg/datasource"
	"github.com/agusibrahim/apksig-go/pkg/signer"
	"github.com/agusibrahim/apksig-go/pkg/v1signer"
	zippkg "github.com/agusibrahim/apksig-go/pkg/zip"
)

// signAPK applies APK Signature Schemes v1+v2+v3 via apksig-go.
func signAPK(unsigned []byte) ([]byte, error) {
	cert, key, err := debugCert()
	if err != nil {
		return nil, err
	}
	alg, err := algo.PickAlgorithm(key)
	if err != nil {
		return nil, fmt.Errorf("pick algorithm: %w", err)
	}
	cfg := &signer.SignerConfig{
		PrivateKey: key,
		Certs:      []*x509.Certificate{cert},
		Algorithms: []algo.Algorithm{alg},
	}

	src := datasource.NewBytes(unsigned)
	withV1, err := injectV1(src, key, cert)
	if err != nil {
		return nil, fmt.Errorf("v1: %w", err)
	}

	var out bytes.Buffer
	w := &apkwriter.SignedAPKWriter{
		Src:      withV1,
		Signers:  []*signer.SignerConfig{cfg},
		V3MinSdk: 28,
		V3MaxSdk: 0x7fffffff,
		Align:    true,
	}
	if err := w.Write(&out); err != nil {
		return nil, fmt.Errorf("v2/v3: %w", err)
	}
	return out.Bytes(), nil
}

func debugCert() (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "GoFront Debug", Organization: []string{"GoFront"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(30, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// injectV1 adds META-INF JAR signature files (glue adapted from apksig-go cmd/apksign).
func injectV1(src datasource.DataSource, priv crypto.PrivateKey, cert *x509.Certificate) (datasource.DataSource, error) {
	eocd, err := zippkg.FindEOCD(src)
	if err != nil {
		return nil, fmt.Errorf("find EOCD: %w", err)
	}
	entries, err := zippkg.ParseCD(src, eocd)
	if err != nil {
		return nil, fmt.Errorf("parse CD: %w", err)
	}

	v1out, err := v1signer.Sign(src, entries, &v1signer.SignerConfig{
		PrivateKey: priv,
		Cert:       cert,
		Name:       "CERT",
	})
	if err != nil {
		return nil, err
	}

	beforeEnd := eocd.CDStartOffset
	if blk, err := apksigblock.Find(src, eocd); err == nil {
		beforeEnd = blk.StartOffset
	}
	origEntries, err := datasource.ReadAll(src.Slice(0, beforeEnd))
	if err != nil {
		return nil, err
	}

	metaFiles := []struct {
		name string
		data []byte
	}{
		{"META-INF/MANIFEST.MF", v1out.Manifest},
		{"META-INF/CERT.SF", v1out.SF},
		{"META-INF/CERT" + v1out.Extension, v1out.PKCS7},
	}

	var metaLFH, metaCD []byte
	metaOffset := uint32(len(origEntries))
	for _, mf := range metaFiles {
		lfh, cdEntry := makeRawZipEntry(mf.name, mf.data, metaOffset)
		metaLFH = append(metaLFH, lfh...)
		metaCD = append(metaCD, cdEntry...)
		metaOffset += uint32(len(lfh))
	}

	origCD, err := datasource.ReadAll(src.Slice(eocd.CDStartOffset, eocd.CDSize))
	if err != nil {
		return nil, err
	}
	filteredCD := filterRawCD(origCD, entries, isV1SignatureFile)
	keptCount := countKept(entries, isV1SignatureFile)

	var out bytes.Buffer
	out.Write(origEntries)
	out.Write(metaLFH)
	cdOff := uint32(out.Len())
	out.Write(filteredCD)
	out.Write(metaCD)
	cdSize := uint32(out.Len()) - cdOff
	totalEntries := uint16(keptCount + len(metaFiles))

	eocdRec := make([]byte, 22)
	binary.LittleEndian.PutUint32(eocdRec[0:4], 0x06054b50)
	binary.LittleEndian.PutUint16(eocdRec[8:10], totalEntries)
	binary.LittleEndian.PutUint16(eocdRec[10:12], totalEntries)
	binary.LittleEndian.PutUint32(eocdRec[12:16], cdSize)
	binary.LittleEndian.PutUint32(eocdRec[16:20], cdOff)
	out.Write(eocdRec)

	return datasource.NewBytes(out.Bytes()), nil
}

func makeRawZipEntry(name string, data []byte, lfhOffset uint32) (lfh []byte, cdEntry []byte) {
	crc := crc32.ChecksumIEEE(data)
	nb := []byte(name)
	lfh = make([]byte, 30+len(nb)+len(data))
	binary.LittleEndian.PutUint32(lfh[0:4], 0x04034b50)
	binary.LittleEndian.PutUint16(lfh[4:6], 20)
	binary.LittleEndian.PutUint16(lfh[8:10], 0)
	binary.LittleEndian.PutUint32(lfh[14:18], crc)
	binary.LittleEndian.PutUint32(lfh[18:22], uint32(len(data)))
	binary.LittleEndian.PutUint32(lfh[22:26], uint32(len(data)))
	binary.LittleEndian.PutUint16(lfh[26:28], uint16(len(nb)))
	copy(lfh[30:], nb)
	copy(lfh[30+len(nb):], data)

	cdEntry = make([]byte, 46+len(nb))
	binary.LittleEndian.PutUint32(cdEntry[0:4], 0x02014b50)
	binary.LittleEndian.PutUint16(cdEntry[4:6], 20)
	binary.LittleEndian.PutUint16(cdEntry[6:8], 20)
	binary.LittleEndian.PutUint16(cdEntry[10:12], 0)
	binary.LittleEndian.PutUint32(cdEntry[16:20], crc)
	binary.LittleEndian.PutUint32(cdEntry[20:24], uint32(len(data)))
	binary.LittleEndian.PutUint32(cdEntry[24:28], uint32(len(data)))
	binary.LittleEndian.PutUint16(cdEntry[28:30], uint16(len(nb)))
	binary.LittleEndian.PutUint32(cdEntry[42:46], lfhOffset)
	copy(cdEntry[46:], nb)
	return
}

func filterRawCD(rawCD []byte, entries []zippkg.CDEntry, skipFn func(string) bool) []byte {
	var out []byte
	off := int64(0)
	for _, e := range entries {
		n := e.HeaderSize
		if !skipFn(e.Name) {
			out = append(out, rawCD[off:off+n]...)
		}
		off += n
	}
	return out
}

func countKept(entries []zippkg.CDEntry, skipFn func(string) bool) int {
	n := 0
	for _, e := range entries {
		if !skipFn(e.Name) {
			n++
		}
	}
	return n
}

func isV1SignatureFile(name string) bool {
	if !strings.HasPrefix(name, "META-INF/") {
		return false
	}
	if name == "META-INF/MANIFEST.MF" {
		return true
	}
	return strings.HasSuffix(name, ".SF") ||
		strings.HasSuffix(name, ".RSA") ||
		strings.HasSuffix(name, ".DSA") ||
		strings.HasSuffix(name, ".EC")
}
