package collection

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

const (
	src  = "https://upload.wikimedia.org/wikipedia/commons/c/ca/1x1.png"
	cdn1 = "https://cdn.shopify.com/s/files/1/1/collections/1x1.png?v=1"
	cdn2 = "https://cdn.shopify.com/s/files/1/1/collections/1x1.png?v=2"
)

func str(s string) *string { return &s }

func img(src, alt, url string) *imageModel {
	m := &imageModel{Src: types.StringValue(src), AltText: types.StringNull(), URL: types.StringValue(url)}
	if alt != "" {
		m.AltText = types.StringValue(alt)
	}
	return m
}

func TestReadImage(t *testing.T) {
	cases := map[string]struct {
		prior   *imageModel
		live    *client.CollectionFieldsImage
		written *string
		want    *imageModel
	}{
		"no image":                  {img(src, "", cdn1), nil, str(cdn1), nil},
		"unchanged keeps src":       {img(src, "A", cdn1), &client.CollectionFieldsImage{Url: cdn1, AltText: str("A")}, str(cdn1), img(src, "A", cdn1)},
		"replaced in the admin":     {img(src, "A", cdn1), &client.CollectionFieldsImage{Url: cdn2, AltText: str("A")}, str(cdn1), img(cdn2, "A", cdn2)},
		"import takes the live url": {nil, &client.CollectionFieldsImage{Url: cdn1}, nil, img(cdn1, "", cdn1)},
		// An alt edit bumps ?v= like a new image does (checked), so it can't be told apart and takes the live URL.
		"alt edited in the admin":     {img(src, "A", cdn1), &client.CollectionFieldsImage{Url: cdn2, AltText: str("B")}, str(cdn1), img(cdn2, "B", cdn2)},
		"empty alt stays empty":       {nil, &client.CollectionFieldsImage{Url: cdn1, AltText: str("")}, nil, &imageModel{Src: types.StringValue(cdn1), AltText: types.StringValue(""), URL: types.StringValue(cdn1)}},
		"nothing remembered from src": {img(src, "", cdn1), &client.CollectionFieldsImage{Url: cdn1}, nil, img(cdn1, "", cdn1)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := readImage(c.prior, c.live, c.written); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestImageInput(t *testing.T) {
	cases := map[string]struct {
		cfg, prior *imageModel
		want       *client.ImageInput
	}{
		"unmanaged":                  {nil, img(src, "A", cdn1), nil},
		"new image":                  {img(src, "A", ""), nil, &client.ImageInput{Src: str(src), AltText: str("A")}},
		"new image without alt":      {img(src, "", ""), nil, &client.ImageInput{Src: str(src)}},
		"unchanged":                  {img(src, "A", ""), img(src, "A", cdn1), nil},
		"src changed":                {img(cdn2, "A", ""), img(src, "A", cdn1), &client.ImageInput{Src: str(cdn2), AltText: str("A")}},
		"alt changed only":           {img(src, "B", ""), img(src, "A", cdn1), &client.ImageInput{AltText: str("B")}},
		"alt unset is unmanaged":     {img(src, "", ""), img(src, "A", cdn1), nil},
		"alt cleared to empty":       {&imageModel{Src: types.StringValue(src), AltText: types.StringValue(""), URL: types.StringNull()}, img(src, "A", cdn1), &client.ImageInput{AltText: str("")}},
		"image removed in the admin": {img(src, "A", ""), nil, &client.ImageInput{Src: str(src), AltText: str("A")}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := imageInput(c.cfg, c.prior); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
