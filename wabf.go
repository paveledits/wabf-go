package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"encoding/csv"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"math/rand"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var (
	disableCache = flag.Bool("disable-cache", false, "Disable session caching")
	outputFormat = flag.String("output-format", "wa.me", "Result output format (wa.me, jid, pn)")
	outputFile   = flag.String("output-file", "", "Specify output file")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	reset        = flag.Bool("reset", false, "Reset session (log out) before starting")
	delay        = flag.Duration("delay", 200*time.Millisecond, "Delay between checks (per worker)")
	concurrency  = flag.Int("concurrency", 1, "Number of parallel workers")
	saveAvatars  = flag.Bool("save-avatars", false, "Download and save profile pictures")
	vcardFile    = flag.String("vcard", "", "Export results to a VCard (.vcf) file")
	csvFile      = flag.String("csv", "", "Export results to a CSV file")
)

type ScanResult struct {
	JID          string
	Phone        string
	Link         string
	Status       string
	Name         string 
	VerifiedName string
	Business     *types.BusinessProfile
	AvatarURL    string
	AvatarPath   string
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "WhatsApp Brute Forcer (Go)\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <phone_pattern>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Parameters:\n")
		fmt.Fprintf(os.Stderr, "  <phone_pattern>  Target Pattern (e.g. 15551234567[x] or +1 555 ...)\n")
		fmt.Fprintf(os.Stderr, "                   Supports single numbers (e.g. +15551234567) too.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		
		fmt.Fprintf(os.Stderr, "  -concurrency <int>\n")
		fmt.Fprintf(os.Stderr, "        Number of parallel workers (default 1)\n")
		fmt.Fprintf(os.Stderr, "  -csv <filename.csv>\n")
		fmt.Fprintf(os.Stderr, "        Export results to a CSV file\n")
		fmt.Fprintf(os.Stderr, "  -vcard <filename.vcf>\n")
		fmt.Fprintf(os.Stderr, "        Export results to a VCard (.vcf) file\n")
		fmt.Fprintf(os.Stderr, "  -delay <duration>\n")
		fmt.Fprintf(os.Stderr, "        Delay between checks (per worker) (default 200ms)\n")
		fmt.Fprintf(os.Stderr, "  -save-avatars\n")
		fmt.Fprintf(os.Stderr, "        Download and save profile pictures\n")
		fmt.Fprintf(os.Stderr, "  -output-file <filename>\n")
		fmt.Fprintf(os.Stderr, "        Specify output file\n")
		fmt.Fprintf(os.Stderr, "  -output-format <format>\n")
		fmt.Fprintf(os.Stderr, "        Result output format (wa.me, jid, pn) (default \"wa.me\")\n")
		fmt.Fprintf(os.Stderr, "  -disable-cache\n")
		fmt.Fprintf(os.Stderr, "        Disable session caching\n")
		fmt.Fprintf(os.Stderr, "  -reset\n")
		fmt.Fprintf(os.Stderr, "        Reset session (log out) before starting\n")
		fmt.Fprintf(os.Stderr, "  -verbose\n")
		fmt.Fprintf(os.Stderr, "        Enable verbose logging\n")

		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  Standard:   %s \"15551234567[x]\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Parallel:   %s -concurrency 4 \"155512345xx\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Export:     %s -csv results.csv -save-avatars \"15551234[5-9]x\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Reset:      %s -reset \"...\"\n", os.Args[0])
	}
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 && !*reset {
		flag.Usage()
		os.Exit(1)
	}
	phonePattern := strings.Join(args, "")
	phonePattern = strings.ReplaceAll(phonePattern, " ", "")
	phonePattern = strings.ReplaceAll(phonePattern, "+", "")

	if len(phonePattern) > 0 && !strings.Contains(phonePattern, "[") && !strings.Contains(phonePattern, "x") {
		clean := strings.ReplaceAll(strings.ReplaceAll(phonePattern, "+", ""), " ", "")
		if _, err := strconv.Atoi(clean); err != nil {
			fmt.Printf("Error: Invalid phone number pattern: '%s'\n", phonePattern)
			fmt.Println("Please provide a valid number or pattern (digits, +, spaces, [ ], x).")
			os.Exit(1)
		}
	}

	var dbLog, clientLog waLog.Logger
	if *verbose {
		dbLog = waLog.Stdout("Database", "WARN", true)
		clientLog = waLog.Stdout("Client", "DEBUG", true)
		log.Printf("Starting wabf with pattern: %s", phonePattern)
	} else {
		dbLog = waLog.Noop
		clientLog = waLog.Noop
	}

	dbPath := "file:wabf.db?_foreign_keys=on"
	
	if *reset {
		if !*verbose {
			fmt.Println("[-] Resetting session (deleting wabf.db)...")
		} else {
			log.Println("Resetting session...")
		}
		os.Remove("wabf.db")
	}
	if *disableCache {
		dbPath = "file::memory:?_foreign_keys=on"
		if *verbose {
			log.Println("Cache disabled, using in-memory database")
		}
	} else if *verbose {
		log.Printf("Using database cache at %s", dbPath)
	}

	container, err := sqlstore.New(context.Background(), "sqlite3", dbPath, dbLog)
	if err != nil {
		if *verbose {
			log.Fatalf("Failed to connect to database: %v", err)
		} else {
			fmt.Printf("Error: Failed to connect to database: %v\n", err)
			os.Exit(1)
		}
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		if *verbose {
			log.Fatalf("Failed to get device: %v", err)
		} else {
			fmt.Printf("Error: Failed to get device: %v\n", err)
			os.Exit(1)
		}
	}

	client := whatsmeow.NewClient(deviceStore, clientLog)

	if !*verbose {
		fmt.Println("WhatsApp Brute Forcer (Go)")
		fmt.Println("--------------------------")
		if phonePattern != "" {
			fmt.Printf("Target Pattern: %s\n", phonePattern)
		} else if *reset {
			fmt.Println("Mode:           Reset Session")
		}
		if *outputFile != "" {
			fmt.Printf("Output File:    %s\n", *outputFile)
		}
		fmt.Println("--------------------------")
	}

	if client.Store.ID == nil {
		fmt.Println("[-] Session not found. Please scan the QR code below to log in.")
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			log.Fatalf("Failed to connect: %v", err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("Scan the QR code to log in")
			} else {
				if *verbose {
					fmt.Println("Login event:", evt.Event)
				}
			}
		}
	} else {
		if !*verbose {
			fmt.Printf("[-] Logged in as: %s\n", client.Store.ID)
		}
		err = client.Connect()
		if err != nil {
			log.Fatalf("Failed to connect: %v", err)
		}
		if *verbose {
			fmt.Println("Logged in as", client.Store.ID)
		}
	}

	var historySyncDone = make(chan bool)
	client.AddEventHandler(func(evt interface{}) {
		switch evt.(type) {
		case *events.HistorySync:
			if *verbose {
				log.Println("Received History Sync event")
			}
			select {
			case historySyncDone <- true:
			default:
			}
		}
	})

	if client.Store.ID == nil {
		go func() {
			select {
			case <-historySyncDone:
				if *verbose {
					log.Println("History Sync received.")
				}
			case <-time.After(30 * time.Second):
			}
		}()
	}

	client.SendPresence(context.Background(), types.PresenceAvailable)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	if phonePattern == "" {
		if !*verbose {
			fmt.Println("[-] No pattern provided. Exiting.")
		}
		client.Disconnect()
		return
	}

	if *verbose {
		log.Println("Generating JIDs...")
	}
	jids, err := generateJIDs(phonePattern)
	if err != nil {
		log.Fatalf("Error generating JIDs: %v", err)
	}
	
	if !*verbose {
		fmt.Printf("[-] Generated %d numbers to check.\n", len(jids))
		fmt.Println("[-] Starting scan...")
	} else {
		fmt.Printf("Generated %d possible JIDs. Starting brute force...\n", len(jids))
	}


	ctx := context.Background()

	var fileOut *os.File
	if *outputFile != "" {
		if *verbose {
			log.Printf("Opening output file: %s", *outputFile)
		}
		fileOut, err = os.Create(*outputFile)
		if err != nil {
			log.Fatalf("Failed to create output file: %v", err)
		}
		defer fileOut.Close()
	}
	

	jidChan := make(chan string, len(jids))
	resultChan := make(chan ScanResult, len(jids))
	var wg sync.WaitGroup
	var checkedCount int64 

	if *concurrency < 1 {
		*concurrency = 1
	}
	if !*verbose {
		fmt.Printf("[-] Starting scan with %d workers...\n", *concurrency)
	}

	totalJIDs := len(jids)

	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for jid := range jidChan {
				select {
				case <-c:
					return
				default:
				}

				current := atomic.AddInt64(&checkedCount, 1)
				if !*verbose {
					percent := float64(current) / float64(totalJIDs) * 100
					pn := strings.TrimSuffix(jid, "@c.us")
					fmt.Printf("[%3.0f%%] Checked: %-15s\n", percent, pn)
				}

				res := checkJID(ctx, client, jid)
				if res != nil {
					resultChan <- *res
				}
			}
		}(w)
	}

	go func() {
		for _, jid := range jids {
			jidChan <- jid
		}
		close(jidChan)
	}()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var results []ScanResult
	foundCount := 0

	var csvWriter *csv.Writer
	var csvF *os.File
	if *csvFile != "" {
		csvF, err = os.Create(*csvFile)
		if err != nil {
			log.Fatalf("Failed to create CSV file: %v", err)
		}
		defer csvF.Close()
		csvWriter = csv.NewWriter(csvF)
		csvWriter.Write([]string{"Phone", "Link", "Status", "Name", "VerifiedName", "Email", "Website", "Address", "AvatarURL"})
		defer csvWriter.Flush()
	}

	var vcfF *os.File
	if *vcardFile != "" {
		vcfF, err = os.Create(*vcardFile)
		if err != nil {
			log.Fatalf("Failed to create VCard file: %v", err)
		}
		defer vcfF.Close()
	}

	if *saveAvatars {
		os.Mkdir("avatars", 0755)
	}

	for res := range resultChan {
		foundCount++
		results = append(results, res)

		if !*verbose {
			fmt.Printf("[+] FOUND: %s\n", res.Link)
			if res.Status != "" {
				fmt.Printf("    Status: %s\n", res.Status)
			}
			if res.Name != "" {
				fmt.Printf("    Name: %s\n", res.Name)
			}
			if res.VerifiedName != "" {
				fmt.Printf("    Verified Name: %s\n", res.VerifiedName)
			}
			if res.Business != nil {
				if res.Business.Email != "" {
					fmt.Printf("    Email: %s\n", res.Business.Email)
				}
				if res.Business.Address != "" {
					fmt.Printf("    Address: %s\n", res.Business.Address)
				}
			}
			if res.AvatarURL != "" {
				fmt.Printf("    Avatar: %s\n", res.AvatarURL)
				if res.AvatarPath != "" {
					fmt.Printf("    -> Saved to: %s\n", res.AvatarPath)
				}
			}
		} else {
			fmt.Printf("FOUND: %s (Info: %+v)\n", res.Link, res)
		}

		if fileOut != nil {
			fmt.Fprintln(fileOut, res.Link)
		}

		if csvWriter != nil {
			email := ""
			website := ""
			address := ""
			if res.Business != nil {
				email = res.Business.Email
				address = res.Business.Address
			}
			csvWriter.Write([]string{
				res.Phone, res.Link, res.Status, res.Name, res.VerifiedName, email, website, address, res.AvatarURL,
			})
		}

		if vcfF != nil {
			name := res.Name
			if name == "" {
				if res.VerifiedName != "" {
					name = res.VerifiedName
				} else {
					name = res.Phone
				}
			}
			vcard := fmt.Sprintf("BEGIN:VCARD\nVERSION:3.0\nFN:%s\nTEL;TYPE=CELL:%s\n", name, "+" + res.Phone)
			if res.AvatarURL != "" {
				vcard += fmt.Sprintf("URL:%s\n", res.AvatarURL)
			}
			if res.Business != nil {
				if res.Business.Email != "" {
					vcard += fmt.Sprintf("EMAIL:%s\n", res.Business.Email)
				}
			}
			vcard += "END:VCARD\n"
			vcfF.WriteString(vcard)
		}
	}

	fmt.Println("\n[-] Scan finished.")
	fmt.Printf("[-] Total found: %d\n", foundCount)

	client.Disconnect()
}

func checkJID(ctx context.Context, client *whatsmeow.Client, jid string) *ScanResult {
	time.Sleep(*delay + time.Duration(rand.Intn(100))*time.Millisecond)

	pn := strings.TrimSuffix(jid, "@c.us")
	if pn == "" {
		return nil
	}
	
	resp, err := client.IsOnWhatsApp(ctx, []string{pn})
	if err != nil {
		if !*verbose {
		} else {
			log.Printf("Error checking %s: %v", pn, err)
		}
		return nil
	}

	if len(resp) > 0 && resp[0].IsIn {
		res := &ScanResult{
			JID:   jid,
			Phone: pn,
			Link:  "https://wa.me/" + strings.ReplaceAll(strings.ReplaceAll(pn, " ", ""), "+", ""),
		}

		targetJID, _ := types.ParseJID(resp[0].JID.String())

		contact, err := client.Store.Contacts.GetContact(ctx, targetJID)
		if err == nil && contact.Found {
			res.Name = contact.FullName
			if res.Name == "" {
				res.Name = contact.PushName
			}
		}

		userInfo, err := client.GetUserInfo(ctx, []types.JID{targetJID})
		if err == nil {
			if info, ok := userInfo[targetJID]; ok {
				res.Status = info.Status
				if resp[0].VerifiedName != nil && resp[0].VerifiedName.Details != nil && resp[0].VerifiedName.Details.VerifiedName != nil {
					res.VerifiedName = *resp[0].VerifiedName.Details.VerifiedName
				}
			}
		}
		
		biz, err := client.GetBusinessProfile(ctx, targetJID)
		if err == nil {
			res.Business = biz
		}

		pic, err := client.GetProfilePictureInfo(ctx, targetJID, &whatsmeow.GetProfilePictureParams{})
		if err == nil && pic != nil {
			res.AvatarURL = pic.URL
			if *saveAvatars {
				path := filepath.Join("avatars", pn+".jpg")
				if err := downloadFile(pic.URL, path); err == nil {
					res.AvatarPath = path
				}
			}
		}

		return res
	}
	return nil
}

func downloadFile(url string, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err

}

func generateJIDs(pattern string) ([]string, error) {
	if !strings.Contains(pattern, "[") && !strings.Contains(pattern, "x") {
		return []string{pattern + "@c.us"}, nil
	}

	if strings.Count(pattern, "[") != strings.Count(pattern, "]") {
		return nil, fmt.Errorf("balanced brackets required")
	}

	re := regexp.MustCompile(`(x|\[\d+?\])`)
	matches := re.FindAllString(pattern, -1)
	parts := re.Split(pattern, -1)

	var fills [][]string
	for _, m := range matches {
		if m == "x" {
			fills = append(fills, strings.Split("0123456789", ""))
		} else {
			options := strings.TrimSuffix(strings.TrimPrefix(m, "["), "]")
			fills = append(fills, strings.Split(options, ""))
		}
	}

	combinations := cartesianProduct(fills)

	var results []string
	for _, combo := range combinations {
		var sb strings.Builder
		for i := 0; i < len(combo); i++ {
			sb.WriteString(parts[i])
			sb.WriteString(combo[i])
		}
		sb.WriteString(parts[len(combo)])
		results = append(results, sb.String()+"@c.us")
	}
	return results, nil
}

func cartesianProduct(input [][]string) [][]string {
	if len(input) == 0 {
		return [][]string{}
	}
	if len(input) == 1 {
		var result [][]string
		for _, s := range input[0] {
			result = append(result, []string{s})
		}
		return result
	}

	rest := cartesianProduct(input[1:])
	var result [][]string
	for _, s := range input[0] {
		for _, r := range rest {
			newRow := append([]string{s}, r...)
			result = append(result, newRow)
		}
	}
	return result
}

func formatOutput(jid, format string) string {
	pn := strings.TrimSuffix(jid, "@c.us")
	cleanPN := strings.ReplaceAll(strings.ReplaceAll(pn, " ", ""), "+", "")
	
	switch format {
	case "wa.me":
		return "https://wa.me/" + cleanPN
	case "jid":
		return jid
	case "pn":
		return cleanPN
	default:
		return "https://wa.me/" + cleanPN
	}
}