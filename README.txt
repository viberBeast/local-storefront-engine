STOREFRONT ENGINE - SYSTEM DOCUMENTATION
========================================

ARCHITECTURE OVERVIEW:
The system employs a dual-channel network security model to isolate public-facing storefront assets from sensitive administrative control functions.

NETWORK INTERFACES:
1. PUBLIC CHANNEL (Port 8080): 
   - Serving storefront via 0.0.0.0.
   - Public access to product catalog, checkout, and order history.
   - Designed for external customer interaction.

2. PRIVATE CHANNEL (Port 8081): 
   - Serving admin dashboard via 127.0.0.1 (Localhost only).
   - Provides administrative "Control Plane" access.
   - Dedicated local route for internal inventory mutations.

RECENT UPDATES (2026-05-26):
- Implemented isolated administrative UI dashboard at http://127.0.0.1:8081.
- Resolved CORS security blocks by localizing the inventory API within the private router multiplexer.
- Normalized database seeding with categorized catalog assets (ACCESSORIES).
- Refined brutalist frontend CSS for cleaner asset/placeholder presentation.
- Standardized binary build and cleanup process to resolve asset embedding inconsistencies.

OPERATIONAL NOTES:
- Use 'pkill -f storefront-engine' to terminate active process instances before rebuilds.
- Use 'go clean -cache' to force binary recompilation when updating embedded web assets.
