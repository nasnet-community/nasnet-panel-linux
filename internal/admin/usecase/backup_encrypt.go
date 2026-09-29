package usecase

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
)

// EncryptBackupFile gzips srcPath and encrypts the stream with an age scrypt
// (passphrase) recipient, writing the result to dstPath. Decrypt with:
// age -d dstPath | gunzip
func EncryptBackupFile(srcPath, dstPath, passphrase string) (err error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return fmt.Errorf("create age recipient: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open dump: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create encrypted file: %w", err)
	}
	defer func() {
		if cerr := dst.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close encrypted file: %w", cerr)
		}
		if err != nil {
			os.Remove(dstPath)
		}
	}()

	encWriter, err := age.Encrypt(dst, recipient)
	if err != nil {
		return fmt.Errorf("start age encryption: %w", err)
	}
	gzWriter := gzip.NewWriter(encWriter)

	if _, err = io.Copy(gzWriter, src); err != nil {
		return fmt.Errorf("compress and encrypt: %w", err)
	}
	if err = gzWriter.Close(); err != nil {
		return fmt.Errorf("flush gzip: %w", err)
	}
	if err = encWriter.Close(); err != nil {
		return fmt.Errorf("finalize age encryption: %w", err)
	}
	return nil
}

// GzipBackupFile writes a gzip copy of srcPath for sites that have not set a
// backup passphrase. Decompress with: gunzip file.sql.gz
func GzipBackupFile(srcPath, dstPath string) (err error) {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	zw := gzip.NewWriter(out)
	if _, err = io.Copy(zw, in); err != nil {
		return err
	}
	return zw.Close()
}
