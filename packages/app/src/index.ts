export { NotaApp, NotaProviders } from "./App.tsx";
export { AuthGate } from "./auth/AuthGate.tsx";
export { ConnectPage, type ConnectRequest } from "./auth/Connect.tsx";
export { DeviceList } from "./auth/Devices.tsx";
export { DevicesPage } from "./auth/DevicesPage.tsx";
export { useSession } from "./auth/session.ts";
export { type AuthMode, SignInFlow } from "./auth/SignIn.tsx";
// The wizard loads as its own chunk (components/lazy.tsx).
export { Onboarding } from "./components/lazy.tsx";
export { CaptureApp } from "./components/Capture.tsx";
export { registerBuiltins } from "./builtins/index.tsx";
export {
  createRegistry,
  Registry,
  useContributions,
  useRegistry,
  type Command,
  type CommandContext,
  type InboxSuggester,
  type Panel,
  type SidebarSection,
  type Suggestion,
  type TreeDecoration,
  type TreeDecorator,
  type TreeItem,
} from "./registry.ts";
export {
  NOTE_MODE_KEY,
  loadNoteMode,
  readNoteMode,
  saveNoteMode,
  toggledNoteMode,
  watchNoteMode,
  type NoteMode,
} from "./lib/noteMode.ts";
export { useUI, type ListSpec, type Route } from "./state.ts";
export {
  createThemeStore,
  DEFAULT_PALETTE,
  PALETTE_KEY,
  readMode,
  resolveMode,
  THEME_KEY,
  themeStore,
  type ResolvedMode,
  type ThemeEnv,
  type ThemeMode,
  type ThemeState,
  type ThemeStore,
} from "./theme/store.ts";
export { useTheme } from "./theme/useTheme.ts";
export { BUILTIN_THEMES, contrast, type ColorTheme, type ThemeColors } from "./theme/themes.ts";
