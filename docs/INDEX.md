# Project Documentation Index

Welcome to the documentation for the **Crypto Trade Level Bot**. This guide provides a deep dive into the project's design and implementation.

## 📖 Table of Contents

1.  **[Architecture](ARCHITECTURE.md)**
    *   Clean Architecture layers (Domain, Usecase, Infrastructure).
    *   Dependency management and interface design.

2.  **[Project Structure](PROJECT_STRUCTURE.md)**
    *   File and directory layout.
    *   What each package is responsible for.

3.  **[Trading Strategy](TRADING_STRATEGY.md)**
    *   Explanation of the "Defending" (Counter-Trend) strategy.
    *   Tier calculations and execution logic.

4.  **[API Integration](API_INTEGRATION.md)**
    *   Interaction with Bybit V5 (REST & WebSocket).
    *   Authentication and error handling.

5.  **[Database Schema](DATABASE_SCHEMA.md)**
    *   SQLite table definitions and relationships.
    *   Persistence for levels, trades, and history.

6.  **[Web Dashboard](WEB_DASHBOARD.md)**
    *   Overview of the management UI.
    *   HTMX dynamic updates and API endpoints.

7.  **[Glossary](GLOSSARY.md)**
    *   Definitions for trading and project-specific terms.

8.  **[Testing Guide](TESTING_GUIDE.md)**
    *   How to run and write unit/integration tests.

---

## 🚀 Getting Started

If you are new to the project, start with the **[README](../README.md)** for installation and running instructions, then dive into the **[Trading Strategy](TRADING_STRATEGY.md)** to understand how the bot makes decisions.
