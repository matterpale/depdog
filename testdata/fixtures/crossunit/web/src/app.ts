// The web app: one legal use of the shared lib, one identity-channel import of
// its exported surface, and one relative reach into its internals (the
// cross-unit surface violation).
import { secret } from "../../shared/internal/secret";
import { shared } from "@acme/shared";
import { util } from "@acme/shared/src/util";

export function app(): number {
  return secret + shared + util;
}
