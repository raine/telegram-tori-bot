package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/raine/telegram-tori-bot/internal/tori"
)

func main() {
	query := flag.String("q", "", "Search query")
	rows := flag.Int("rows", 10, "Number of results")
	page := flag.Int("page", 0, "Page number (0-indexed)")
	location := flag.String("location", "", "Location filter")
	category := flag.String("category", "", "Category filter (e.g., 0.93)")
	subCategory := flag.String("sub-category", "", "Sub-category filter (e.g., 1.93.3215)")
	productCategory := flag.String("product-category", "", "Product category filter (e.g., 2.93.3215.8368)")
	rawJSON := flag.Bool("json", false, "Output raw JSON only")
	flag.Parse()

	client := tori.NewSearchClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	params := tori.SearchParams{
		Query:    *query,
		Rows:     *rows,
		Page:     *page,
		Location: *location,
	}

	// Set category taxonomy (only one should be used)
	if *productCategory != "" {
		params.CategoryTaxonomy = tori.CategoryTaxonomy{
			ParamName: "product_category",
			Value:     *productCategory,
		}
	} else if *subCategory != "" {
		params.CategoryTaxonomy = tori.CategoryTaxonomy{
			ParamName: "sub_category",
			Value:     *subCategory,
		}
	} else if *category != "" {
		params.CategoryTaxonomy = tori.CategoryTaxonomy{
			ParamName: "category",
			Value:     *category,
		}
	}

	results, err := client.Search(ctx, tori.SearchKeyBapCommon, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *rawJSON {
		jsonBytes, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(jsonBytes))
		return
	}

	fmt.Printf("Found %d results (total: %d)\n\n", len(results.Docs), results.Metadata.ResultSize.MatchCount)

	for i, doc := range results.Docs {
		price := "N/A"
		if doc.Price != nil {
			price = fmt.Sprintf("%d€", doc.Price.Amount)
		}
		fmt.Printf("%d. %s - %s\n", i+1, doc.Heading, price)
		if doc.Location != "" {
			fmt.Printf("   %s\n", doc.Location)
		}
	}
}
