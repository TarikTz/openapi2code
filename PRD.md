# Product Requirements Document (PRD): **OpenAPI2Code** (v6.0)

## 1. Product Overview & Naming

* **Product Name:** **OpenAPI2Code** (Domain: `OpenAPI2Code.com`)
* **Overview:** A high-performance, zero-dependency multi-target code generation engine and web playground written in Go. OpenAPI2Code ingests OpenAPI/Swagger (v2 & v3) specifications from local files, raw strings, or remote HTTP URLs and instantly compiles them into robust, production-ready types and validation schemas for modern web and mobile ecosystems.

---

## 2. Target Audience

* Full-stack engineers building web applications (.NET/Go backends paired with React/Next.js/Zod frontends).
* Mobile developers (iOS/Android/Flutter) consuming backend API contracts.
* Teams seeking ultra-fast, lightweight type synchronization without relying on heavy Node.js runtimes.

---

## 3. Core Features & Capabilities

### 3.1. Go Core Engine (CLI & WASM Library)

* **Multi-Source Input Parsing:** Supports local file paths, raw text payloads, and remote HTTP/HTTPS URLs (e.g., live staging/dev .NET API endpoints).
* **Multi-Target Code Generation:**
* **TypeScript Interfaces (`.ts`):** Modular multi-file directory output (Proto-style with barrel `index.ts`) or monolithic single-file output (`--out-file`).
* **Runtime Validation Schemas (Zod):** Generates matching Zod parser schemas (`z.object({...})`) alongside TypeScript types for end-to-end type safety and form validation.
* **Mobile Data Models (Swift / Kotlin / Dart):** Generates native type-safe models (`Codable` structs for Swift, `data class` for Kotlin, and classes for Dart) to seamlessly bridge backend APIs to mobile apps.


* **Zero-Dependency Footprint:** Compiled down to a single standalone static binary.

### 3.2. Web Playground UI (`OpenAPI2Code.com` / WASM-Powered)

* **Split-Screen Workspace:**
* **Left Pane:** Input configuration panel supporting URL fetching, file drop, or raw text pasting, plus **Target Language Selectors** (TypeScript, Zod, Swift, Kotlin, Dart).
* **Right Pane:** Real-time generated code output with a tabbed file viewer (for multi-file outputs) and a **"Copy to Clipboard"** action.


* **Client-Side Execution:** Compiles the Go engine to WebAssembly (`.wasm`), allowing zero-latency conversions directly in the browser.

---

## 4. Technical Architecture

```
[ Local File ] ──┐
[ Raw String ] ──┼──> ┌───────────────────────────────────────────────┐
[ Remote URL ] ──┘    │            OpenAPI2Code Go Engine             │ 
                      └───────────────────────────────────────────────┘
                              │                               │
                              ▼ (CLI Binary)                  ▼ (WASM Compilation)
                      ┌───────────────────────┐       ┌───────────────────────┐
                      │ Modular Code Output   │       │   Web Playground UI   │
                      │ • TypeScript (`.ts`)  │       │   (OpenAPI2Code.com   │
                      │ • Zod Schemas         │       │    Multi-Target)      │
                      │ • Swift / Kotlin/Dart │       └───────────────────────┘
                      └───────────────────────┘

```

---

## 5. User Flow

1. **For Local/CI Development:** Developer runs the CLI tool pointing directly to a dev environment endpoint (`openapi2code pull [https://dev.api.com/swagger.json](https://dev.api.com/swagger.json) --target ts,zod --output ./src/types`), instantly updating frontend contracts without Node dependencies.
2. **For Mobile Teams:** Mobile engineers target native models (`openapi2code pull [https://api.com/swagger.json](https://api.com/swagger.json) --target swift --output ./iOS/Models`), keeping iOS/Android apps in sync with backend schema adjustments.
3. **For Quick Inspection (`OpenAPI2Code.com`):** Developer opens the web playground, pastes a staging OpenAPI URL, selects their target language/schema format, and copies the resulting code instantly.