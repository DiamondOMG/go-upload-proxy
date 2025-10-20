package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
)

const (
	PORT              = 5001
	TARGETR_UPLOAD_URL = "https://stacks.targetr.net/upload"
	MAX_UPLOAD_SIZE    = 100 * 1024 * 1024 // 100MB
)

// CORS Middleware
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-UploadType, X-FileName, X-ItemType, X-PendingId, X-ItemId")

		// Handle preflight OPTIONS request
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// Upload Handler - รับไฟล์จาก Client และส่งต่อไป TargetR
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	// ตรวจสอบว่าเป็น POST method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// จำกัดขนาดไฟล์
	r.Body = http.MaxBytesReader(w, r.Body, MAX_UPLOAD_SIZE)

	// อ่าน Body ทั้งหมดเป็น Buffer
	fileBuffer, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// ตรวจสอบว่า Body ไม่ว่างเปล่า
	if len(fileBuffer) == 0 {
		http.Error(w, "File body is missing or empty.", http.StatusBadRequest)
		return
	}

	// สร้าง Request ใหม่สำหรับส่งไป TargetR
	proxyReq, err := http.NewRequest("POST", TARGETR_UPLOAD_URL, bytes.NewReader(fileBuffer))
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		http.Error(w, "Internal Server Error during proxy.", http.StatusInternalServerError)
		return
	}

	// คัดลอก Headers จาก Request ต้นฉบับ
	proxyReq.Header.Set("X-UploadType", getHeaderWithDefault(r, "X-UploadType", "raw"))
	proxyReq.Header.Set("X-FileName", r.Header.Get("X-FileName"))
	proxyReq.Header.Set("X-ItemType", getHeaderWithDefault(r, "X-ItemType", "libraryitem"))
	proxyReq.Header.Set("Content-Type", getHeaderWithDefault(r, "Content-Type", "content/unknown"))
	proxyReq.Header.Set("X-PendingId", r.Header.Get("X-PendingId"))
	proxyReq.Header.Set("X-ItemId", r.Header.Get("X-ItemId"))
	// ⚠️ ต้องเพิ่ม Basic Auth/Credentials ของ TargetR ใน Production
	// proxyReq.Header.Set("Authorization", "Basic [TargetR Credentials]")

	log.Printf("[Proxy] Receiving %d bytes. Forwarding to TargetR...", len(fileBuffer))

	// ส่ง Request ไป TargetR
	client := &http.Client{}
	targetrResp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("Error forwarding to TargetR: %v", err)
		http.Error(w, "Internal Server Error during proxy.", http.StatusInternalServerError)
		return
	}
	defer targetrResp.Body.Close()

	// อ่าน Response จาก TargetR
	responseBody, err := io.ReadAll(targetrResp.Body)
	if err != nil {
		log.Printf("Error reading TargetR response: %v", err)
		http.Error(w, "Error reading TargetR response.", http.StatusInternalServerError)
		return
	}

	// ตรวจสอบสถานะจาก TargetR
	if targetrResp.StatusCode != http.StatusOK {
		log.Printf("[TargetR Error]: %d %s", targetrResp.StatusCode, string(responseBody))
		http.Error(w, fmt.Sprintf("TargetR Error: %s", string(responseBody)), targetrResp.StatusCode)
		return
	}

	// ส่ง Response ที่สำเร็จกลับไป Client
	w.WriteHeader(http.StatusOK)
	w.Write(responseBody)
}

// Helper function สำหรับดึง Header พร้อม Default Value
func getHeaderWithDefault(r *http.Request, key, defaultValue string) string {
	value := r.Header.Get(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func main() {
	// ตั้งค่า Route
	http.HandleFunc("/upload-go", corsMiddleware(uploadHandler))

	// Start Server
	addr := fmt.Sprintf(":%d", PORT)
	log.Printf("Go Upload Proxy listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}