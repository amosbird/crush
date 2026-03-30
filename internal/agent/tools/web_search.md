Search the web using DuckDuckGo and return search results with titles, URLs, and snippets.

<usage>
- Provide a search query to find information on the web
- Returns a list of search results with titles, URLs, and snippets
- Use this to find relevant web pages, then use fetch or agentic_fetch to get full content from promising URLs
</usage>

<parameters>
- query: The search query string (required)
- max_results: Maximum number of results to return (default: 10, max: 20)
</parameters>

<tips>
- Use specific, targeted search queries for better results (3-6 words is often ideal)
- After getting results, use fetch to get the full content of relevant pages
- Combine multiple searches to gather comprehensive information on complex topics
- If results aren't relevant, try rephrasing with different keywords
- For library documentation, consider using the context7 MCP tool instead if available
</tips>
