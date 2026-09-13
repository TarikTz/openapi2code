// examples-content.js — populates the static code samples on
// examples.html. Every string below is real, verified output: generated
// by an actual `openapi2code pull` run against the spec shown just above
// it on the page (see the commit that added this file for the exact
// commands), not hand-written or hand-edited afterward. This page has
// no WASM module of its own — loading one just to render five fixed
// snippets would be needless weight for a static showcase page, so the
// output is pasted in verbatim and highlighted with the same
// highlightCode() the playground itself uses, for a consistent look.
import { highlightCode } from "./highlight.js";

if (window.lucide) {
  window.lucide.createIcons();
}

const PET_OUTPUTS = {
  ts: {
    label: "TypeScript",
    lang: "ts",
    code: `export interface Category {
  id?: number;
  name: string;
}

export interface Error {
  code: number;
  message: string;
}

export interface Pet {
  category?: Category;
  id?: number;
  name: string;
  photoUrls: string[];
  status?: "available" | "pending" | "sold";
  tags?: Tag[];
}

export interface Tag {
  id?: number;
  name: string;
}
`,
  },
  zod: {
    label: "Zod",
    lang: "ts",
    code: `import { z } from "zod";

export const CategorySchema = z.object({ id: z.number().optional(), name: z.string(), });
export type Category = z.infer<typeof CategorySchema>;

export const ErrorSchema = z.object({ code: z.number(), message: z.string(), });
export type Error = z.infer<typeof ErrorSchema>;

export const TagSchema = z.object({ id: z.number().optional(), name: z.string(), });
export type Tag = z.infer<typeof TagSchema>;

export const PetSchema = z.object({ category: CategorySchema.optional(), id: z.number().optional(), name: z.string(), photoUrls: z.array(z.string()), status: z.enum(["available", "pending", "sold"]).optional(), tags: z.array(TagSchema).optional(), });
export type Pet = z.infer<typeof PetSchema>;
`,
  },
  swift: {
    label: "Swift",
    lang: "swift",
    code: `public struct Pet: Codable {
    public var category: Category?
    public var id: Int?
    public var name: String
    public var photoUrls: [String]
    public var status: PetStatus?
    public var tags: [Tag]?

    enum CodingKeys: String, CodingKey {
        case category = "category"
        case id = "id"
        case name = "name"
        case photoUrls = "photoUrls"
        case status = "status"
        case tags = "tags"
    }
}

public enum PetStatus: String, Codable {
    case available
    case pending
    case sold
}
`,
  },
  kotlin: {
    label: "Kotlin",
    lang: "kotlin",
    code: `data class Pet(
    val category: Category? = null,
    val id: Int? = null,
    val name: String,
    val photoUrls: List<String>,
    val status: PetStatus? = null,
    val tags: List<Tag>? = null
) {
    fun toJson(): Map<String, Any?> {
        val map = mutableMapOf<String, Any?>()
        map["category"] = category?.toJson()
        map["id"] = id
        map["name"] = name
        map["photoUrls"] = photoUrls.map { it }
        map["status"] = status?.value
        map["tags"] = tags?.map { it.toJson() }
        return map
    }

    companion object {
        fun fromJson(json: Map<String, Any?>): Pet {
            return Pet(
                category = (json["category"] as? Map<String, Any?>)?.let { Category.fromJson(it) },
                id = (json["id"] as? Number)?.toInt(),
                name = json["name"] as String,
                photoUrls = (json["photoUrls"] as List<*>).map { it as String },
                status = (json["status"] as? String)?.let { PetStatus.fromValue(it) },
                tags = (json["tags"] as? List<*>)?.map { Tag.fromJson(it as Map<String, Any?>) }
            )
        }
    }
}

enum class PetStatus(val value: String) {
    AVAILABLE("available"),
    PENDING("pending"),
    SOLD("sold");

    companion object {
        fun fromValue(value: String): PetStatus = values().first { it.value == value }
    }
}
`,
  },
  dart: {
    label: "Dart",
    lang: "dart",
    code: `class Pet {
    final Category? category;
    final int? id;
    final String name;
    final List<String> photoUrls;
    final PetStatus? status;
    final List<Tag>? tags;

    Pet({
        this.category,
        this.id,
        required this.name,
        required this.photoUrls,
        this.status,
        this.tags,
    });

    factory Pet.fromJson(Map<String, dynamic> json) {
        return Pet(
            category: json["category"] != null ? Category.fromJson(json["category"] as Map<String, dynamic>) : null,
            id: json["id"] as int?,
            name: json["name"] as String,
            photoUrls: (json["photoUrls"] as List<dynamic>).map((e) => e as String).toList(),
            status: json["status"] != null ? PetStatus.fromValue(json["status"] as String) : null,
            tags: (json["tags"] as List<dynamic>?)?.map((e) => Tag.fromJson(e as Map<String, dynamic>)).toList(),
        );
    }

    Map<String, dynamic> toJson() {
        return {
            "category": category?.toJson(),
            "id": id,
            "name": name,
            "photoUrls": photoUrls.map((e) => e).toList(),
            "status": status?.value,
            "tags": tags?.map((e) => e.toJson()).toList(),
        };
    }
}

enum PetStatus {
    available("available"),
    pending("pending"),
    sold("sold");

    final String value;
    const PetStatus(this.value);

    static PetStatus fromValue(String value) => PetStatus.values.firstWhere((e) => e.value == value);
}
`,
  },
};

function renderInto(elementId, code, lang) {
  const el = document.getElementById(elementId);
  if (el) {
    el.innerHTML = highlightCode(code, lang);
  }
}

// --- Pet: tabbed across all 5 targets ---

const petOutput = document.getElementById("pet-output");
const petTabs = document.getElementById("pet-tabs");
let activePetTarget = "ts";

function renderPetTabs() {
  petTabs.innerHTML = "";
  for (const key of Object.keys(PET_OUTPUTS)) {
    const tab = document.createElement("button");
    tab.type = "button";
    tab.textContent = PET_OUTPUTS[key].label;
    const isActive = key === activePetTarget;
    tab.className = isActive
      ? "rounded-sm px-3 py-1 text-sm font-mono bg-brace dark:bg-brace-light text-white dark:text-ink"
      : "rounded-sm px-3 py-1 text-sm font-mono border border-ink/15 dark:border-paper/15 hover:bg-ink/5 dark:hover:bg-paper/10";
    tab.addEventListener("click", () => {
      activePetTarget = key;
      renderPetTabs();
      renderPetOutput();
    });
    petTabs.appendChild(tab);
  }
}

function renderPetOutput() {
  const entry = PET_OUTPUTS[activePetTarget];
  petOutput.innerHTML = highlightCode(entry.code, entry.lang);
}

renderPetTabs();
renderPetOutput();

// --- allOf: Dog ---

renderInto(
  "dog-ts",
  `export interface Animal {
  name: string;
}

export interface Dog extends Animal {
  breed: string;
}
`,
  "ts"
);
renderInto(
  "dog-swift",
  `public struct Animal: Codable {
    public var name: String

    enum CodingKeys: String, CodingKey {
        case name = "name"
    }
}

public struct Dog: Codable {
    public var name: String
    public var breed: String

    enum CodingKeys: String, CodingKey {
        case name = "name"
        case breed = "breed"
    }
}
`,
  "swift"
);

// --- oneOf: StringOrNumber ---

renderInto("union-ts", `export type StringOrNumber = string | number;\n`, "ts");
renderInto(
  "union-swift",
  `// StringOrNumber was not generated for the Swift target: unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type).\n`,
  "swift"
);

// --- Circular: TreeNode ---

renderInto(
  "tree-ts",
  `export interface TreeNode {
  children?: TreeNode[];
  value: string;
}
`,
  "ts"
);
renderInto(
  "tree-swift",
  `public class TreeNode: Codable {
    public var children: [TreeNode]?
    public var value: String

    enum CodingKeys: String, CodingKey {
        case children = "children"
        case value = "value"
    }

    public init(children: [TreeNode]?, value: String) {
        self.children = children
        self.value = value
    }

    public required init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.children = try container.decodeIfPresent([TreeNode].self, forKey: .children)
        self.value = try container.decode(String.self, forKey: .value)
    }
}
`,
  "swift"
);
