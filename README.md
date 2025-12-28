# WhatsApp Brute Forcer (Go)

A fast, robust, and feature-rich WhatsApp number scanner and bio fetcher, written in Go.

This tool allows you to brute-force generate phone numbers based on a pattern, check if they are registered on WhatsApp, and fetch their public profile information (Status/About, Avatar, PushName, Business Info).

## 🚀 Features

*   **Pattern Matching**: Generate numbers using wildcards (`x`) or ranges (`[0-9]`).
    *   **Wildcard `x`**: Iterates through digits 0-9.
        *   Example: `1555123456x` -> checks `...60` to `...69` (10 numbers).
    *   **Set `[...]`**: Iterates through the specific digits provided inside the brackets.
        *   Example: `155512345[123]` -> checks `...51`, `...52`, `...53` (3 numbers).
    *   **Combination**: You can mix them.
        *   Example: `1555[12]xxxx` -> checks 20,000 numbers.
*   **Parallel Scanning**: Ultra-fast scanning with concurrent workers (`-concurrency`).
*   **Profile Intelligence**: Fetches:
    *   ✅ **Status / About** text.
    *   ✅ **Profile Pictures** (HD URLs).
    *   ✅ **Push Names** (~Name) and Verified Business Names.
    *   ✅ **Business Info** (Email, Website, Address).
*   **Smart Exporting**:
    *   **CSV**: Export structured data for analysis.
    *   **VCard (.vcf)**: Generate contacts file to import directly into your phone.
    *   **Avatar Saver**: Automatically download profile pictures to `avatars/`.
*   **Privacy Aware**: Respects local contacts (prioritizes local store) and handles privacy settings gracefully.
*   **Stealthy**: Default random delays and user-agent mimicking to avoid rate limits.

## 📦 Installation

Prerequisites: [Go](https://go.dev/dl/) installed.

```bash
# Clone the repository (or download the source)
git clone https://github.com/paveledits/wabf-go
cd wabf-go

# Build the binary
go build -o wabf wabf.go
```

## 🛠 Usage

Basic usage:
```bash
./wabf "<phone_number_pattern>"
```

### Options

| Flag | Description | Default |
| :--- | :--- | :--- |
| `-concurrency` | Number of parallel worker threads | `1` |
| `-delay` | Delay between checks (e.g. `200ms`, `1s`) | `200ms` |
| `-csv` | Save results to a CSV file | (disabled) |
| `-vcard` | Save results to a `.vcf` contact file | (disabled) |
| `-save-avatars`| Download profile pictures to `./avatars/` | `false` |
| `-verbose` | Enable basic debug logging | `false` |
| `-reset` | Reset session (log out) and re-scan QR | `false` |

### Examples

**1. Scan a range of numbers rapidly:**
```bash
./wabf -concurrency 5 "1555123xxxx"
```

**2. Save Avatars and CSV for analysis:**
```bash
./wabf -save-avatars -csv leads.csv "15551234[5-9]x"
```

**3. Import results to your Phone (VCard):**
```bash
./wabf -vcard new_contacts.vcf "1555123xxxx"
```

**4. Check a single specific number:**
```bash
./wabf "+1 555 1234567"
```

## ⚠️ Disclaimer

This tool is for **educational and research purposes only**. Do not use this tool for spamming, harassment, or any illegal activities. The author is not responsible for any misuse.
