package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const venmoApiUrl = "https://account.venmo.com/api/stories?feedType=me"

// Transaction represents a Venmo transaction.
type Transaction struct {
	ID     string `json:"id"`
	Amount string `json:"amount"`
	Date   string `json:"date"` // RFC3339 format
	Type   string `json:"type"` // transfer or payment
	Note   struct {
		Name    string `json:"name,omitempty"`
		Content string `json:"content,omitempty"`
	} `json:"note"`
	Title struct {
		Payload struct {
			SubType string `json:"subType,omitempty"` // standardTransfer or p2p
		} `json:"payload,omitempty"`
		Receiver struct {
			DisplayName string `json:"displayName,omitempty"`
			Username    string `json:"username,omitempty"`
		} `json:"receiver,omitempty"`
		Sender struct {
			DisplayName string `json:"displayName,omitempty"`
			Username    string `json:"username,omitempty"`
		} `json:"sender,omitempty"`
	} `json:"title,omitempty"`
}

// ResponseData represents the Venmo API response.
type ResponseData struct {
	NextID  string        `json:"nextId"`
	Stories []Transaction `json:"stories"`
}

func main() {
	if len(os.Args) != 4 {
		fmt.Println("Usage: go run main.go <external_id> <stop_transaction_date> <cookie>")
		os.Exit(1)
	}

	externalId := os.Args[1] // external ID
	endDate := os.Args[2]    // YYYY-MM-DD format
	cookie := os.Args[3]     // cookie string

	// Parse end date for comparison later
	endDateParsed, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		log.Fatalf("Failed to parse end date: %v", err)
	}

	// Write the CSV headers
	writer := csv.NewWriter(os.Stdout)
	defer writer.Flush()

	headers := []string{"Amount", "Date", "Note Name", "Note Date"}
	if err := writer.Write(headers); err != nil {
		log.Fatalf("Failed to write headers to CSV: %v", err)
	}

	// Start fetching transactions
	nextID := ""
	for {
		url := venmoApiUrl + "&externalId=" + externalId
		if nextID != "" {
			url += "&nextId=" + nextID
		}

		// Make the HTTP request
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Fatalf("Failed to create HTTP request: %v", err)
		}

		// Set headers
		req.Header.Set("accept", "*/*")
		req.Header.Set("accept-language", "en-US,en;q=0.9")
		req.Header.Set("cookie", cookie)
		req.Header.Set("dnt", "1")
		req.Header.Set("referer", "https://account.venmo.com/")
		req.Header.Set("sec-ch-ua", `"Chromium";v="129", "Not=A?Brand";v="8"`)
		req.Header.Set("sec-ch-ua-mobile", "?0")
		req.Header.Set("sec-ch-ua-platform", `"macOS"`)
		req.Header.Set("user-agent", "Mozilla/5.0")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("Failed to make HTTP request: %v", err)
		}
		defer resp.Body.Close()

		// Read and parse the response
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("Failed to read response body: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			log.Fatalf("Failed to fetch transactions: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		}

		var data ResponseData
		if err := json.Unmarshal(body, &data); err != nil {
			log.Fatalf("Failed to parse JSON response: %v", err)
		}

		// Process each transaction
		for _, story := range data.Stories {
			// Parse the transaction date
			// Example: 2024-10-04T13:28:52
			transactionDate, err := time.Parse(time.RFC3339, story.Date)
			if err != nil {
				// Fallback to another date format if RFC3339 fails
				transactionDate, err = time.Parse("2006-01-02T15:04:05", story.Date)
				if err != nil {
					log.Printf("Failed to parse transaction date: %v", err)
					continue
				}
			}
			if err != nil {
				log.Printf("Failed to parse transaction date: %v", err)
				continue
			}

			// Stop fetching if the transaction is older than the end date
			if transactionDate.Before(endDateParsed) {
				fmt.Println("Reached end date, stopping.")
				return
			}

			var note string
			var transActionType string
			var amount float64
			if strings.Contains(strings.ToLower(story.Type), "transfer") {
				transActionType = "Transfer"
				note = fmt.Sprintf("Transfer %s | %s", story.Note.Name, story.Amount)
				amount = 0
			} else if story.Type == "payment" {
				transActionType = "Payment"
				// parse amount to int from string
				amountStr := strings.ReplaceAll(story.Amount, "$", "")
				amountStr = strings.ReplaceAll(amountStr, ",", "")
				amountStr = strings.ReplaceAll(amountStr, "+", "")
				amountStr = strings.ReplaceAll(amountStr, "-", "")
				amountStr = strings.TrimSpace(amountStr)
				amount, err = strconv.ParseFloat(amountStr, 32)
				if err != nil {
					log.Printf("Failed to parse amount: %v", err)
					continue
				}
				if strings.Contains(story.Amount, "-") {
					amount = -1 * amount
				}
				if story.Title.Sender.DisplayName == "you" {
					receiver := story.Title.Receiver.DisplayName
					if receiver == "" {
						receiver = story.Title.Receiver.Username
					}
					note = fmt.Sprintf("To %s | %s", receiver, story.Note.Content)
				} else {
					sender := story.Title.Sender.DisplayName
					if sender == "" {
						sender = story.Title.Sender.Username
					}
					note = fmt.Sprintf("From %s | %s", sender, story.Note.Content)
				}
			}

			// Write transaction to CSV
			record := []string{
				strconv.FormatFloat(amount, 'f', 2, 64),
				story.Date,
				transActionType,
				note,
			}
			if err := writer.Write(record); err != nil {
				log.Fatalf("Failed to write transaction to CSV: %v", err)
			}
			writer.Flush()
		}

		// Stop if no more transactions to fetch
		if data.NextID == "" {
			fmt.Println("No more transactions to fetch.")
			break
		}
		nextID = data.NextID
	}
}
