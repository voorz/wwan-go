package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	simcard "github.com/voorz/wwan-go/sim/card"
	"github.com/voorz/wwan-go/sim/simfile"
)

type FindAID struct {
	Label    string
	Prefix   []byte
	NotFound error
}

type App struct {
	Reader simcard.Reader
	AID    []byte
}

func (c FindAID) Run(ctx context.Context, r simcard.Reader) ([]byte, error) {
	apps, err := r.ListApplications(ctx)
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		if len(app.AID) == 0 {
			continue
		}
		if strings.EqualFold(app.Label, c.Label) || (len(c.Prefix) > 0 && bytes.HasPrefix(app.AID, c.Prefix)) {
			return slices.Clone(app.AID), nil
		}
	}

	if c.NotFound != nil {
		return nil, c.NotFound
	}
	return nil, errors.New("application not found")
}

func (a App) Transparent(ctx context.Context, path []byte) ([]byte, error) {
	file := a.file(path)
	attrs, err := a.fileStructure(ctx, file, simfile.StructureTransparent)
	if err != nil {
		return nil, err
	}

	data, err := a.Reader.ReadTransparent(ctx, simcard.TransparentRead{
		File:   file,
		Length: attrs.FileSize,
	})
	if err != nil {
		return nil, fmt.Errorf("reading transparent file %X: %w", path, err)
	}
	return slices.Clone(data), nil
}

func (a App) Text(ctx context.Context, path []byte) (simfile.Text, error) {
	data, err := a.Transparent(ctx, path)
	if err != nil {
		return "", err
	}

	var text simfile.Text
	if err := text.UnmarshalBinary(data); err != nil {
		return "", fmt.Errorf("reading text file %X: %w", path, err)
	}
	return text, nil
}

func (a App) LinearFixed(ctx context.Context, path []byte) ([][]byte, error) {
	file := a.file(path)
	attrs, err := a.fileStructure(ctx, file, simfile.StructureLinearFixed)
	if err != nil {
		return nil, err
	}
	if attrs.RecordCount == 0 {
		return nil, fmt.Errorf("reading linear fixed file %X: file has no records", path)
	}

	records := make([][]byte, 0, attrs.RecordCount)
	for recordID := uint16(1); recordID <= attrs.RecordCount; recordID++ {
		record, err := a.Reader.ReadRecord(ctx, simcard.RecordRead{
			File:   file,
			Record: recordID,
			Length: attrs.RecordSize,
		})
		if err != nil {
			return nil, fmt.Errorf("reading linear fixed file %X record %d: %w", path, recordID, err)
		}
		records = append(records, slices.Clone(record))
	}
	return records, nil
}

func (a App) FirstText(ctx context.Context, path []byte) (simfile.Text, error) {
	file := a.file(path)
	attrs, err := a.fileStructure(ctx, file, simfile.StructureLinearFixed)
	if err != nil {
		return "", err
	}
	if attrs.RecordCount == 0 {
		return "", fmt.Errorf("reading text records from file %X: file has no records", path)
	}

	for recordID := uint16(1); recordID <= attrs.RecordCount; recordID++ {
		record, err := a.Reader.ReadRecord(ctx, simcard.RecordRead{
			File:   file,
			Record: recordID,
			Length: attrs.RecordSize,
		})
		if err != nil {
			return "", fmt.Errorf("reading text record %d from file %X: %w", recordID, path, err)
		}

		var value simfile.Text
		if err := value.UnmarshalBinary(record); err != nil {
			continue
		}
		if value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("reading text records from file %X: no populated record", path)
}

func (a App) file(path []byte) simcard.FileRef {
	return simcard.FileRef{
		AID:  slices.Clone(a.AID),
		Path: slices.Clone(path),
	}
}

func (a App) fileStructure(ctx context.Context, file simcard.FileRef, want simfile.FileStructure) (simcard.FileAttributes, error) {
	attrs, err := a.Reader.FileAttributes(ctx, file)
	if err != nil {
		return simcard.FileAttributes{}, fmt.Errorf("reading file %X attributes: %w", file.Path, err)
	}
	if attrs.FileStructure != want {
		return simcard.FileAttributes{}, fmt.Errorf("reading file %X attributes: structure %X, want %X", file.Path, attrs.FileStructure, want)
	}
	return attrs, nil
}
