# LLM API Usage Guide (For Agents)

This document provides agents and developers with detailed instructions on how to interact with this LLM FastAPI service. This API is compatible with OpenAI's data schemas and primarily provides health checks and streaming chat completions via Server-Sent Events (SSE).

## Base Information

- **OpenAPI Version**: 3.1.0
    
- **Base URL**: _(Replace with your actual server URL)_
    
- **Data Formats**: `application/json` (Requests), `text/event-stream` (Chat Responses)
    

## 1. Health Check Endpoint

Indicates whether the LLM service is ready to accept requests.

- **Endpoint**: `/healthz`
    
- **Method**: `GET`
    
- **Operation ID**: `healthz_healthz_get`
    

### Response (200 OK)

```
{
  "status": "ok"
}
```

## 2. Chat Completions Endpoint

Stream chat completions using OpenAI-compatible models. Responses are returned incrementally as Server-Sent Events (SSE) with delta updates.

- **Endpoint**: `/chat`
    
- **Method**: `POST`
    
- **Operation ID**: `chat_chat_post`
    

### Request Body

A JSON payload conforming to the `ChatRequest` schema is required.

**Required Fields**:

- `messages` (Array): An array of message objects representing the conversation history.
    
- `stream` (Boolean): Whether to use streaming (should generally be set to `true`).
    

**Optional Fields**:

- `model` (String | null): The name of the model to use.
    

#### Messages Format

The `messages` array can contain messages from various roles:

1. **Developer / System Message (`developer` / `system`)**
    
    - Used to provide global instructions to the model. _(Note: `developer` is preferred over `system` for o1 models and newer)_.
        
    - Example: `{"role": "developer", "content": "You are a helpful assistant."}`
        
2. **User Message (`user`)**
    
    - Prompts or context from the end-user.
        
    - The `content` can be a simple string or an array for **multimodal inputs**:
        
        - **Plain Text**: `{"type": "text", "text": "Hello"}`
            
        - **Image Input**: `{"type": "image_url", "image_url": {"url": "https://...", "detail": "auto"}}`
            
        - **Audio Input**: `{"type": "input_audio", "input_audio": {"data": "<base64_string>", "format": "wav"}}`
            
        - **File Input**: `{"type": "file", "file": {"file_id": "...", "filename": "..."}}`
            
3. **Assistant Message (`assistant`)**
    
    - Historical responses from the model. It may include plain text or tool invocations (`tool_calls`).
        
4. **Tool / Function Message (`tool` / `function`)**
    
    - The result of an external tool or function execution. Must include the corresponding `tool_call_id`.
        

### Example Request (POST /chat)

```
{
  "model": "gpt-4o",
  "stream": true,
  "messages": [
    {
      "role": "developer",
      "content": "You are a helpful AI assistant."
    },
    {
      "role": "user",
      "content": "Explain the basics of quantum mechanics."
    }
  ]
}
```

### Response Format (SSE Event Stream)

**Successful Response (200 OK)**

The content type will be `text/event-stream`. Data is returned in chunks as delta updates:

```
{
  "delta": "Quantum",
  "isFinished": false
}
...
{
  "delta": " mechanics...",
  "isFinished": false
}
...
{
  "delta": "",
  "isFinished": true
}
```

### Error Handling

- **502 Bad Gateway**: Failed to communicate with the backend OpenAI API.
    
    ```
    {
      "detail": "Error while communicating with the OpenAI API: Connection timeout"
    }
    ```
    
- **422 Validation Error**: The request payload does not match the required schema (e.g., missing fields or invalid format). Returns an array of errors detailing the `loc` (location) and `msg` (message).
    

## Implementation Notes for Agents

1. **Mandatory Streaming Processing**: Since the `/chat` endpoint is specifically designed to return SSE, Agents must be capable of parsing `text/event-stream` and listening continuously until `isFinished` evaluates to `true`.
    
2. **Multimodal Support**: Based on the OpenAPI spec, the API supports images (`image_url`), audio (`input_audio`), and files (`file`). If an Agent needs to handle multimodal prompts, it must construct the `content` property using the array structure defined in `ChatCompletionUserMessageParam`.
    
3. **Tool Calling Handling**: If the model's response includes `tool_calls`, the Agent must parse this request, execute the corresponding custom tool/function locally, and append the result back into the `messages` array using `role: "tool"` before initiating the next request.