import * as LosslessJSON from "lossless-json";
import pako from "pako";
import { deserialize, serialize } from "./vendor/palconf/uesave/uesave_wasm.js";
import { ENTRIES, type Entry } from "./vendor/palconf/entries";
import zhCN from "./vendor/palconf/zh-CN.json";
import { WORLD_OPTION_CATEGORIES } from "./worldOptionCategories";
import type { PalworldSettings, Setting, SettingGroup, WorldOptionDocument } from "./api";

const SECRET_MASK = "••••••••";
const SENSITIVE_KEYS = new Set(["AdminPassword", "ServerPassword"]);
const ENUM_TYPES: Record<string, string> = {
  DeathPenalty: "EPalOptionWorldDeathPenalty",
  Difficulty: "EPalOptionWorldDifficulty",
  LogFormatType: "EPalOptionWorldLogFormatType",
  RandomizerType: "EPalRandomizerType",
  CrossplayPlatforms: "EPalAllowConnectPlatform",
  DenyTechnologyList: "EPalDenyTechnology"
};

type GvasValue = Record<string, any>;

export type ParsedWorldOption = {
  document: WorldOptionDocument;
  magic: number;
  gvas: any;
  originalValues: Record<string, string>;
  presentKeys: Set<string>;
};

const entryLabels = (zhCN as any).translation.entry.name as Record<string, string>;

export function parseWorldOption(document: WorldOptionDocument): ParsedWorldOption {
  const data = base64ToBytes(document.data);
  if (data.length < 12) throw new Error("WorldOption.sav 文件过短");
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const compressedLength = view.getUint32(4, true);
  const magic = view.getUint32(8, true);
  if (compressedLength !== data.length - 12) throw new Error("WorldOption.sav 压缩长度不匹配");
  let decompressed = data.slice(12);
  const compression = data[11];
  if (compression === 0x32) {
    decompressed = pako.inflate(pako.inflate(decompressed));
  } else if (compression === 0x31) {
    decompressed = pako.inflate(decompressed);
  } else {
    throw new Error("WorldOption.sav 使用了不支持的压缩格式");
  }
  const gvas = LosslessJSON.parse(deserialize(decompressed, new Map()));
  const struct = settingsStruct(gvas);
  const originalValues: Record<string, string> = {};
  const presentKeys = new Set<string>();
  for (const [key, entry] of Object.entries(ENTRIES)) {
    if (struct[key] !== undefined) presentKeys.add(key);
    originalValues[key] = decodeEntry(struct[key], entry);
  }
  return { document, magic, gvas, originalValues, presentKeys };
}

export function worldOptionSettings(parsed: ParsedWorldOption): PalworldSettings {
  const usedKeys = new Set<string>();
  const groups: SettingGroup[] = WORLD_OPTION_CATEGORIES.map((category) => {
    const settings = category.keys.flatMap((key) => {
      const entry = ENTRIES[key];
      if (!entry) return [];
      usedKeys.add(key);
      return [toSetting(entry, parsed.originalValues[key], parsed.presentKeys.has(key))];
    });
    return { id: category.id, label: category.label, description: category.description, settings };
  }).filter((group) => group.settings.length > 0);

  const unclassified = Object.keys(ENTRIES).filter((key) => !usedKeys.has(key));
  if (unclassified.length > 0) {
    groups.push({
      id: "version_additions",
      label: "版本新增功能",
      description: "解析器已识别、但尚未归入稳定类别的新版本选项",
      settings: unclassified.map((key) =>
        toSetting(ENTRIES[key], parsed.originalValues[key], parsed.presentKeys.has(key))
      )
    });
  }
  return {
    version: "WorldOption 1.0",
    revision: parsed.document.revision,
    groups,
    raw: "",
    lastModified: parsed.document.lastModified
  };
}

export function encodeWorldOption(
  parsed: ParsedWorldOption,
  settings: PalworldSettings,
  changedKeys: ReadonlySet<string>
): { data: string; semantic: string } {
  const gvas = LosslessJSON.parse(LosslessJSON.stringify(parsed.gvas) ?? "{}");
  const struct = settingsStruct(gvas);
  for (const group of settings.groups) {
    for (const setting of group.settings) {
      if (!changedKeys.has(setting.key)) continue;
      const entry = ENTRIES[setting.key];
      if (!entry) throw new Error(`WorldOption.sav 不支持参数 ${setting.key}`);
      const current = canonicalValue(setting.value, entry);
      if (SENSITIVE_KEYS.has(setting.key) && current === SECRET_MASK) {
        throw new Error(`${setting.key} 必须显式输入新值`);
      }
      struct[setting.key] = encodeEntry(current, entry);
    }
  }

  const semantic = LosslessJSON.stringify(gvas) ?? "{}";
  let serialized = serialize(semantic);
  const uncompressedLength = serialized.length;
  const compression = (parsed.magic >>> 24) & 0xff;
  if (compression === 0x32) {
    serialized = pako.deflate(pako.deflate(serialized));
  } else if (compression === 0x31) {
    serialized = pako.deflate(serialized);
  }
  const output = new Uint8Array(12 + serialized.length);
  const view = new DataView(output.buffer);
  view.setUint32(0, uncompressedLength, true);
  view.setUint32(4, serialized.length, true);
  view.setUint32(8, parsed.magic, true);
  output.set(serialized, 12);
  return { data: bytesToBase64(output), semantic };
}

export function verifyWorldOptionData(document: WorldOptionDocument, expectedSemantic: string): void {
  const reparsed = parseWorldOption(document);
  const actualSemantic = LosslessJSON.stringify(reparsed.gvas) ?? "{}";
  if (!worldOptionSemanticsEqual(expectedSemantic, actualSemantic)) {
    throw new Error("WorldOption.sav 往返校验失败，已拒绝保存");
  }
}

function worldOptionSemanticsEqual(expectedSemantic: string, actualSemantic: string): boolean {
  if (expectedSemantic === actualSemantic) return true;
  const expected = LosslessJSON.parse(expectedSemantic);
  const actual = LosslessJSON.parse(actualSemantic);
  return gvasValuesEqual(expected, actual, []);
}

function gvasValuesEqual(expected: any, actual: any, path: string[]): boolean {
  if (isFloatValuePath(path)) {
    const expectedNumber = Number(String(expected));
    const actualNumber = Number(String(actual));
    return Number.isFinite(expectedNumber) &&
      Number.isFinite(actualNumber) &&
      expectedNumber === actualNumber;
  }
  if (expected === null || actual === null ||
      typeof expected !== "object" || typeof actual !== "object") {
    return LosslessJSON.stringify(expected) === LosslessJSON.stringify(actual);
  }
  if (Array.isArray(expected) || Array.isArray(actual)) {
    if (!Array.isArray(expected) || !Array.isArray(actual) || expected.length !== actual.length) {
      return false;
    }
    return expected.every((item, index) => gvasValuesEqual(item, actual[index], [...path, String(index)]));
  }
  const expectedKeys = Object.keys(expected);
  const actualKeys = Object.keys(actual);
  if (expectedKeys.length !== actualKeys.length ||
      expectedKeys.some((key, index) => key !== actualKeys[index])) {
    return false;
  }
  return expectedKeys.every((key) =>
    gvasValuesEqual(expected[key], actual[key], [...path, key])
  );
}

function isFloatValuePath(path: string[]): boolean {
  return path.length >= 3 &&
    path[path.length - 3] === "Float" &&
    path[path.length - 2] === "value" &&
    path[path.length - 1] === "value";
}

export function verifyWorldOptionRoundTrip(
  parsed: ParsedWorldOption,
  settings: PalworldSettings
): void {
  const encoded = encodeWorldOption(parsed, settings, new Set());
  verifyWorldOptionData({ ...parsed.document, data: encoded.data }, encoded.semantic);
}

function settingsStruct(gvas: any): Record<string, GvasValue> {
  const struct = gvas?.root?.properties?.OptionWorldData?.Struct?.value?.Struct
    ?.Settings?.Struct?.value?.Struct;
  if (!struct || typeof struct !== "object") {
    throw new Error("WorldOption.sav 中缺少 OptionWorldData.Settings");
  }
  return struct;
}

function toSetting(entry: Entry, value: string, configured: boolean): Setting {
  const type = entry.type === "boolean"
    ? "boolean"
    : entry.type === "integer" || entry.type === "float"
      ? "number"
      : entry.type === "select"
        ? "select"
        : SENSITIVE_KEYS.has(entry.id) ? "password" : "text";
  const options = entry.id === "DeathPenalty"
    ? ["None", "Item", "ItemAndEquipment", "All"]
    : entry.id === "Difficulty"
      ? ["Custom", "None", "Normal", "Hard"]
      : [...(entry.options ?? [])];
  if (entry.type === "select" && !options.includes(value)) options.unshift(value);
  const effectiveValue = SENSITIVE_KEYS.has(entry.id) && value ? SECRET_MASK : typedValue(value, entry);
  const defaultValue = typedValue(entry.id === "Difficulty" ? "Custom" : entry.defaultValue, entry);
  return {
    key: entry.id,
    label: entryLabels[entry.id] ?? entry.name,
    description: categoryHint(entry),
    type,
    value: effectiveValue,
    default: defaultValue,
    min: entry.range?.[0],
    max: entry.range?.[1],
    step: entry.type === "integer" ? 1 : entry.type === "float" ? 0.1 : undefined,
    options: options?.map((option) => ({ label: option, value: option })),
    sensitive: SENSITIVE_KEYS.has(entry.id),
    risk: riskFor(entry.id),
    restartRequired: true,
    configured
  };
}

function decodeEntry(value: GvasValue | undefined, entry: Entry): string {
  if (!value) return entry.defaultValue;
  if ("Enum" in value) return String(value.Enum.value).split("::").at(-1) ?? entry.defaultValue;
  if ("Int" in value) return String(value.Int.value);
  if ("Float" in value) return String(value.Float.value);
  if ("Bool" in value) return value.Bool.value ? "True" : "False";
  if ("Str" in value) return String(value.Str.value);
  if ("Array" in value && value.Array.array_type === "EnumProperty") {
    return (value.Array.value.Base.Enum as string[]).map((item) => item.split("::").at(-1)).join(",");
  }
  return entry.defaultValue;
}

function encodeEntry(value: string, entry: Entry): GvasValue {
  const enumType = ENUM_TYPES[entry.id];
  if (entry.type === "select" && enumType) {
    return { Enum: { value: `${enumType}::${value}`, enum_type: enumType } };
  }
  if (entry.type === "array" && enumType) {
    const values = value.trim() ? value.split(",").map((item) => `${enumType}::${item.trim()}`) : [];
    return { Array: { array_type: "EnumProperty", value: { Base: { Enum: values } } } };
  }
  if (entry.type === "boolean") return { Bool: { value: value === "True" } };
  if (entry.type === "integer") return { Int: { value: Number(value) } };
  if (entry.type === "float") return { Float: { value: Number(value) } };
  return { Str: { value } };
}

function typedValue(value: string, entry: Entry): string | number | boolean {
  if (entry.type === "boolean") return value === "True";
  if (entry.type === "integer" || entry.type === "float") return Number(value);
  return value;
}

function canonicalValue(value: string | number | boolean, entry: Entry): string {
  if (entry.type === "boolean") return value === true || value === "True" ? "True" : "False";
  if (entry.type === "integer") return String(Math.trunc(Number(value)));
  if (entry.type === "float") return String(Number(value));
  return String(value);
}

function valuesEqual(left: string, right: string, entry: Entry): boolean {
  if (entry.type === "integer" || entry.type === "float") {
    return Number(left) === Number(right);
  }
  if (entry.type === "boolean") {
    return (left === "True") === (right === "True");
  }
  return left === right;
}

function categoryHint(entry: Entry): string {
  if (entry.desc) return `${entryLabels[entry.id] ?? entry.name}；修改后需重启服务器`;
  return "WorldOption.sav 世界选项；修改后需重启服务器";
}

function riskFor(key: string): "performance" | "disk" | "security" | "" {
  if (/Password|RCON|RESTAPI|PublicIP|BanList|ClientMod/.test(key)) return "security";
  if (/Backup|AutoSave/.test(key)) return "disk";
  if (/MaxNum|MaxBuilding|CullDistance|TickInterval|SpawnNum|MaxGuilds/.test(key)) return "performance";
  return "";
}

function base64ToBytes(value: string): Uint8Array {
  const binary = atob(value);
  const output = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index++) output[index] = binary.charCodeAt(index);
  return output;
}

function bytesToBase64(value: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let index = 0; index < value.length; index += chunkSize) {
    binary += String.fromCharCode(...value.subarray(index, index + chunkSize));
  }
  return btoa(binary);
}
