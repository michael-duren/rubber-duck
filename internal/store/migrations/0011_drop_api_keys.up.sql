-- The agent publish API is gone: course changes now go through the human
-- proposal workflow, and the CLI authenticates with gc_u_ user tokens.
-- Agent API keys have nothing left to authenticate.
DROP TABLE api_keys;
