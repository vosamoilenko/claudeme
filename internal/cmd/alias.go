package cmd

import (
	"fmt"
	"os"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// Alias handles the alias subcommand
func Alias() {
	if len(os.Args) < 3 {
		aliasUsage()
		os.Exit(1)
	}

	switch os.Args[2] {
	case "add", "set":
		aliasAdd()
	case "list", "ls":
		aliasList()
	case "remove", "rm":
		aliasRemove()
	default:
		fmt.Fprintf(os.Stderr, "Unknown alias command: %s\n", os.Args[2])
		aliasUsage()
		os.Exit(1)
	}
}

func aliasUsage() {
	fmt.Println("Usage:")
	fmt.Println("  claudeme alias add <name> <email>  Add an alias for quick switching")
	fmt.Println("  claudeme alias list                List all aliases")
	fmt.Println("  claudeme alias rm <name>           Remove an alias")
	fmt.Println()
	fmt.Println("Example:")
	fmt.Println("  claudeme alias add work volodymyr@company.com")
	fmt.Println("  claudeme alias add personal me@gmail.com")
	fmt.Println("  claudeme use work    # Uses the alias to switch profile")
}

func aliasAdd() {
	if len(os.Args) < 5 {
		fmt.Fprintf(os.Stderr, "Usage: claudeme alias add <name> <email>\n")
		os.Exit(1)
	}

	name := os.Args[3]
	email := os.Args[4]

	// Verify the email is a known profile
	cfg, _ := config.LoadProfiles()
	if _, exists := cfg.Profiles[email]; !exists {
		fmt.Fprintf(os.Stderr, "%s No profile found for %q\n", WarnStyle.Render("!"), email)
		fmt.Fprintf(os.Stderr, "Add it first with: claudeme add\n")
		os.Exit(1)
	}

	aliases, err := config.LoadAliases()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading aliases: %v\n", err)
		os.Exit(1)
	}

	aliases.SetAlias(name, email)

	if err := aliases.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving aliases: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s %s -> %s\n", SuccessStyle.Render("*"), name, email)
}

func aliasList() {
	aliases, err := config.LoadAliases()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading aliases: %v\n", err)
		os.Exit(1)
	}

	if len(aliases.Aliases) == 0 {
		fmt.Println("No aliases configured.")
		fmt.Println(DimStyle.Render("Add one with: claudeme alias add <name> <email>"))
		return
	}

	fmt.Println(HeaderStyle.Render("Aliases:"))
	fmt.Println()
	for name, email := range aliases.Aliases {
		fmt.Printf("  %s -> %s\n", name, email)
	}
}

func aliasRemove() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: claudeme alias rm <name>\n")
		os.Exit(1)
	}

	name := os.Args[3]

	aliases, err := config.LoadAliases()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading aliases: %v\n", err)
		os.Exit(1)
	}

	if !aliases.RemoveAlias(name) {
		fmt.Fprintf(os.Stderr, "Alias not found: %s\n", name)
		os.Exit(1)
	}

	if err := aliases.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving aliases: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s Removed alias: %s\n", SuccessStyle.Render("*"), name)
}
