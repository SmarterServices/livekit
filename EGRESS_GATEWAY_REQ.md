Egress Gateway Specs

We want to modify this service to support only running as an egress gateway for LiveKit. Currently LiveKit Server and LiveKit Egress must be using the same Redis instance. The redis instance that it shares only acts as a control plane for the egress service. So we want to allow the server and the egress service to use different redis instances and operate independently. For this to happen we want to modify this code base to support running as a standalone egress gateway service. We need to make the following changes.

- Allow for a switch to be set to when starting this service to indicate if it is running the full service or just as an egress gateway.
- If running as an egress gateway, we only need the minimal components to run, not all the other stuff.

How the gateway will work.
- The goal is to keep the existing interface and functionality intact, but to allow for the service to run in a standalone mode.
- For this reason, we will interact with the existing LiveKit server through the same LiveKit API, with just one slight modification to the start egress calls.

- When calling start egress, we will want to send the host, and api credentials for the livekit server we want egress to record from. 
- When egress is creating the tokens etc it will use the credentials provided to generate the tokens for the livekit server, so it will properly authenticate with the server.
- When egress called, it always looks for the room to be available on the server, so for this, we need to use the information passed in the API call, to call back to the remote livekit server to check if the room is available. If so, it will use that room information to start the egress. If not it will error just like normal.
- All other existing APIs for egress should work as normal.

Structuring the changes.
- Ideally we want to change as little code as possible. To make it easy to maintain and update, we should create a new package or module for the gateway functionality and then conditionally compile it based on the switch.
- We will be seeing if livekit will pull this back into the main repo, but if not this is will be something we need to maintain.



