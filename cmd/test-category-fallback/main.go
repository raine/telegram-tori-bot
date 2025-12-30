package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/raine/telegram-tori-bot/llm"
	"github.com/raine/telegram-tori-bot/tori"
)

// Rate limit delay between tests (Gemini free tier: 10 req/min)
const rateLimitDelay = 7 * time.Second

// TestCase represents a product to test the fallback with
type TestCase struct {
	Title       string
	Description string
}

var testCases = []TestCase{
	{
		Title:       "Salli Twin satulatuoli, punainen",
		Description: "Laadukas kotimainen Salli Twin -satulatuoli punaisella verhoilulla. Säädettävä korkeus, hyvä ergonomia. Käytetty kotitoimistossa.",
	},
	{
		Title:       "iPhone 13 Pro 256GB",
		Description: "Apple iPhone 13 Pro, 256GB muisti, Sierra Blue väri. Näytönsuoja ollut alusta asti, ei naarmuja. Akun kunto 89%.",
	},
	{
		Title:       "IKEA Kallax hylly 4x4",
		Description: "Valkoinen IKEA Kallax hyllykkö, 4x4 lokeroa. Hyvässä kunnossa, pieniä käytön jälkiä.",
	},
	{
		Title:       "Sony WH-1000XM5 langattomat kuulokkeet",
		Description: "Sony WH-1000XM5 vastamelukuulokkeet, musta. Erinomaisessa kunnossa, alkuperäinen kotelo mukana.",
	},
	{
		Title:       "Giant Trance X 29 maastopyörä",
		Description: "Giant Trance X 29 täysjousitettu maastopyörä, koko L. Fox-jousitus, Shimano XT -vaihteet. Huollettu säännöllisesti.",
	},
	{
		Title:       "Fjällräven Kånken reppu",
		Description: "Klassinen Fjällräven Kånken reppu, tummansininen. Hyvässä kunnossa.",
	},
	{
		Title:       "PlayStation 5 Digital Edition",
		Description: "Sony PlayStation 5 Digital Edition, kaksi ohjainta. Vähän käytetty, toimii moitteettomasti.",
	},
	{
		Title:       "Weber Spirit E-310 kaasukrilli",
		Description: "Weber Spirit E-310 kaasukrilli, 3 poltinta. Käytetty pari kesää, hyvässä kunnossa. Kaasupullo ei kuulu kauppaan.",
	},
	{
		Title:       "Yamaha P-125 digitaalipiano",
		Description: "Yamaha P-125 sähköpiano, musta. Sisältää telineen ja pedaalin. Erinomainen aloittelijalle tai harrastajalle.",
	},
	{
		Title:       "Bugaboo Fox 3 lastenvaunut",
		Description: "Bugaboo Fox 3 yhdistelmävaunut, musta runko, harmaa koppa ja istuin. Sadesuoja ja jalkalämmitin mukana.",
	},
	{
		Title:       "Zotac RTX 3080 Trinity näytönohjain",
		Description: "Zotac Gaming GeForce RTX 3080 Trinity OC, 10GB GDDR6X. Ei louhittu, käytetty pelaamiseen. Takuu voimassa.",
	},
	{
		Title:       "Marimekko Unikko pussilakanat",
		Description: "Marimekko Unikko pussilakana + tyynyliina, punainen. Käyttämätön, alkuperäispakkauksessa.",
	},
}

func main() {
	ctx := context.Background()

	// Initialize Gemini analyzer
	gemini, err := llm.NewGeminiAnalyzer(ctx)
	if err != nil {
		fmt.Printf("Failed to initialize Gemini: %v\n", err)
		fmt.Println("Make sure GEMINI_API_KEY is set")
		os.Exit(1)
	}

	// Initialize category service
	categoryService := tori.NewCategoryService()

	fmt.Println(string(repeatByte('=', 80)))
	fmt.Println("CATEGORY FALLBACK TEST")
	fmt.Println(string(repeatByte('=', 80)))
	fmt.Println()

	for i, tc := range testCases {
		fmt.Printf("─── Test %d ───────────────────────────────────────────────────────────────────\n", i+1)
		fmt.Printf("Title: %s\n", tc.Title)
		fmt.Printf("Description: %s\n\n", truncate(tc.Description, 80))

		// Stage 1: Extract keywords
		keywords, err := gemini.ExtractCategoryKeywords(ctx, tc.Title, tc.Description)
		if err != nil {
			fmt.Printf("❌ Keyword extraction failed: %v\n\n", err)
			continue
		}
		fmt.Printf("📝 Keywords: %v\n", keywords)

		// Stage 2: Search categories
		categories := categoryService.SearchCategories(keywords, 5)
		if len(categories) == 0 {
			fmt.Printf("❌ No categories found\n\n")
			continue
		}

		fmt.Printf("🔍 Found %d categories:\n", len(categories))
		for j, cat := range categories {
			path := tori.GetCategoryPath(cat)
			fmt.Printf("   %d. [%d] %s\n", j+1, cat.ID, path)
		}

		// Stage 3: LLM selection
		selectedID, err := gemini.SelectCategory(ctx, tc.Title, tc.Description, categories)
		if err != nil {
			fmt.Printf("❌ Category selection failed: %v\n\n", err)
			continue
		}

		if selectedID > 0 {
			// Find the selected category
			for _, cat := range categories {
				if cat.ID == selectedID {
					fmt.Printf("✅ Selected: [%d] %s\n", cat.ID, tori.GetCategoryPath(cat))
					break
				}
			}
		} else {
			fmt.Printf("⚠️  LLM returned 0 - no category selected\n")
		}
		fmt.Println()

		// Rate limit delay (skip after last test)
		if i < len(testCases)-1 {
			fmt.Printf("⏳ Waiting %v for rate limit...\n\n", rateLimitDelay)
			time.Sleep(rateLimitDelay)
		}
	}

	fmt.Println(string(repeatByte('=', 80)))
	fmt.Println("TEST COMPLETE")
	fmt.Println(string(repeatByte('=', 80)))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func repeatByte(b byte, n int) []byte {
	result := make([]byte, n)
	for i := range result {
		result[i] = b
	}
	return result
}
