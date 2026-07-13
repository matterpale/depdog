// The shared lib illegally reaches back into the web app: its work-file rule
// is deny ["*"] (a library depends on nobody).
import { app } from "../web/src/app";

export const shared: number = app();
