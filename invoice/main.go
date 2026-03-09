package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type InvoiceRequest struct {
	Text string `json:"text"`
}

type InvoiceResponse struct {
	ExtractedData string `json:"extracted_data"`
}

func processWithPhi(text string) (string, error) {
	prompt := fmt.Sprintf("Extract invoice data from this text: %s\nReturn as JSON with fields: invoice_number, date, amount, vendor, items", text)

	cmd := exec.Command("python3", "-c", fmt.Sprintf(`
from transformers import AutoTokenizer, AutoModelForCausalLM
import torch

tokenizer = AutoTokenizer.from_pretrained("microsoft/Phi-3.5-mini-instruct")
model = AutoModelForCausalLM.from_pretrained("microsoft/Phi-3.5-mini-instruct", torch_dtype=torch.float32)

inputs = tokenizer("%s", return_tensors="pt")
with torch.no_grad():
    outputs = model.generate(**inputs, max_length=512)

result = tokenizer.decode(outputs[0], skip_special_tokens=True)
print(result)
`, strings.ReplaceAll(prompt, `"`, `\"`)))

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func processInvoice(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot open file"})
		return
	}
	defer src.Close()

	content, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot read file"})
		return
	}

	text := string(content)
	if strings.HasSuffix(file.Filename, ".pdf") {
		text = "PDF processing not implemented yet"
	}

	result, err := processWithPhi(text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, InvoiceResponse{ExtractedData: result})
}

func health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func main() {
	// Start email monitoring in background
	go func() {
		log.Println("Starting email monitor...")
		processor := NewEmailProcessor()
		for {
			log.Println("Checking for new emails...")
			if err := processor.ProcessEmails(); err != nil {
				log.Printf("Error processing emails: %v", err)
			}
			time.Sleep(30 * time.Second) // Check every 30 seconds
		}
	}()

	// Start HTTP server
	r := gin.Default()
	r.POST("/process-invoice", processInvoice)
	r.GET("/health", health)

	log.Println("Starting HTTP server on :8000")
	r.Run(":8000")
}