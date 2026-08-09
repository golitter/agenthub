import { describe, expect, it } from "vitest"
import { changedFileCount, contentForFile, updateFileDraft } from "./draft"
import type { ConfigFile } from "./schema"

const file: ConfigFile = {
  id: "backend_config",
  path: "backend/configs/config.yaml",
  examplePath: "backend/configs/config.example.yaml",
  title: "backend/configs/config.yaml",
  kind: "yaml",
  section: "configs",
  exists: true,
  revision: "sha256:old",
  exampleContent: "server:\n  port: 8080\n",
  actualContent: "server:\n  port: 9000\n",
  sameContent: false,
}

describe("file draft", () => {
  it("tracks complete actual file content", () => {
    const draft = updateFileDraft({}, file, "server:\n  port: 9100\n")
    expect(contentForFile(draft, file)).toContain("9100")
    expect(changedFileCount(draft)).toBe(1)
  })

  it("removes the draft when content returns to disk value", () => {
    const changed = updateFileDraft({}, file, "changed")
    expect(updateFileDraft(changed, file, file.actualContent)).toEqual({})
  })
})
