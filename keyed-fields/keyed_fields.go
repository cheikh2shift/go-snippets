package main

import "fmt"

// --- Domain Models ---

type Address struct {
	City  string
	State string
}

type Contact struct {
	Email string
	Phone string
}

// Employee embeds Address and Contact anonymously,
// promoting City, State, Email, and Phone to top-level fields.
type Employee struct {
	Name    string
	Age     int
	Address // anonymous embed
	Contact // anonymous embed
}

// --- Config Models ---

type DatabaseConfig struct {
	Host     string
	Port     int
	Name     string
	SSL      bool
	MaxConns int
}

type CacheConfig struct {
	TTL     int
	MaxSize int
}

// AppConfig embeds DatabaseConfig and CacheConfig anonymously,
// promoting all their fields to top-level keys.
type AppConfig struct {
	DatabaseConfig // anonymous embed
	CacheConfig    // anonymous embed
}

// --- Deep Nesting Model ---

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type Location struct {
	Coordinates // anonymous embed — Latitude & Longitude promoted
	Altitude    float64
}

// Site embeds Location, which embeds Coordinates.
// Latitude, Longitude, and Altitude are all promoted to Site.
type Site struct {
	Name     string
	Location // anonymous embed (2 levels deep)
}

func main() {
	fmt.Println("========================================")
	fmt.Println("  Go 1.27 Keyed Fields: Verbose vs Concise")
	fmt.Println("========================================")
	fmt.Println()

	// ---- Employee: Verbose (Go 1.26 and earlier) ----
	fmt.Println("--- Verbose Style (Go 1.26 and earlier) ---")
	e1 := Employee{
		Name: "Alice",
		Age:  30,
		Address: Address{
			City:  "NYC",
			State: "NY",
		},
		Contact: Contact{
			Email: "alice@example.com",
			Phone: "555-0100",
		},
	}
	fmt.Printf("%+v\n", e1)

	// ---- Employee: Concise (Go 1.27 keyed fields) ----
	fmt.Println()
	fmt.Println("--- Concise Style (Go 1.27 Keyed Fields) ---")
	e2 := Employee{
		Name:  "Bob",
		Age:   25,
		City:  "LA",    // promoted from Address
		State: "CA",    // promoted from Address
		Email: "bob@example.com", // promoted from Contact
		Phone: "555-0200",        // promoted from Contact
	}
	fmt.Printf("%+v\n", e2)

	fmt.Println()
	fmt.Println("Employee: 12 lines -> 8 lines (4 saved, 33% reduction)")

	// ---- AppConfig: Verbose ----
	fmt.Println()
	fmt.Println("--- AppConfig Verbose (Go 1.26) ---")
	c1 := AppConfig{
		DatabaseConfig: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Name:     "mydb",
			SSL:      true,
			MaxConns: 100,
		},
		CacheConfig: CacheConfig{
			TTL:     300,
			MaxSize: 256,
		},
	}
	fmt.Printf("%+v\n", c1)

	// ---- AppConfig: Concise ----
	fmt.Println()
	fmt.Println("--- AppConfig Concise (Go 1.27 Keyed Fields) ---")
	c2 := AppConfig{
		Host:     "localhost",  // promoted from DatabaseConfig
		Port:     5432,         // promoted from DatabaseConfig
		Name:     "mydb",       // promoted from DatabaseConfig
		SSL:      true,         // promoted from DatabaseConfig
		MaxConns: 100,          // promoted from DatabaseConfig
		TTL:      300,          // promoted from CacheConfig
		MaxSize:  256,          // promoted from CacheConfig
	}
	fmt.Printf("%+v\n", c2)

	fmt.Println()
	fmt.Println("AppConfig: 14 lines -> 9 lines (5 saved, 36% reduction)")

	// ---- Deep Nesting (2 levels of promotion) ----
	fmt.Println()
	fmt.Println("--- Deep Nesting: 2 Levels of Promotion ---")
	s := Site{
		Name:     "Data Center",
		Latitude:  40.7128,   // promoted from Coordinates via Location
		Longitude: -74.0060,  // promoted from Coordinates via Location
		Altitude:  10.5,      // promoted from Location
	}
	fmt.Printf("%+v\n", s)
	fmt.Println("Site: 14 lines -> 6 lines (8 saved, 57% reduction)")

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("  Summary")
	fmt.Println("========================================")
	fmt.Println("Go 1.27 keyed fields let you use promoted field")
	fmt.Println("names as struct literal keys, eliminating verbose")
	fmt.Println("nested struct initialization syntax.")
	fmt.Println()
	fmt.Println("Employee:   12 lines -> 8 lines  (4 saved, 33%)")
	fmt.Println("AppConfig:  14 lines -> 9 lines  (5 saved, 36%)")
	fmt.Println("Site:       14 lines -> 6 lines  (8 saved, 57%)")
	fmt.Println()
	fmt.Println("Total: 17 lines saved across 3 structs")
	fmt.Println("========================================")
}
