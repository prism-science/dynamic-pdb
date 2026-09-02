package dynamicpdbapi

import (
	"encoding/json"
	"fmt"
)

const jsonAPIMediaType = "application/vnd.api+json"

type jsonAPIData[T any] struct {
	Attributes T `json:"attributes"`
}

type jsonAPIDataDocument[T any] struct {
	Data jsonAPIData[T] `json:"data"`
}

type jsonAPIDataCollectionDocument[T any] struct {
	Data []jsonAPIData[T] `json:"data"`
}

func decodeAttributes[T any](body []byte) (T, error) {
	var document jsonAPIDataDocument[T]
	if err := json.Unmarshal(body, &document); err != nil {
		var zero T
		return zero, fmt.Errorf("decode JSON:API document: %w", err)
	}
	return document.Data.Attributes, nil
}

func decodeAttributeCollection[T any](body []byte) ([]T, error) {
	var document jsonAPIDataCollectionDocument[T]
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("decode JSON:API collection: %w", err)
	}
	attributes := make([]T, 0, len(document.Data))
	for _, data := range document.Data {
		attributes = append(attributes, data.Attributes)
	}
	return attributes, nil
}
