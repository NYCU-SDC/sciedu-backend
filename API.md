# SciEdu API Specification Document

This document is automatically generated based on the `sciedu-api` TypeSpec specifications, intended as a reference for frontend and backend Agents during implementation.

- **Server Base URL**: `https://dev.sciedu.sdc.nycu.club/api`
    
- **API Version**: `1.0.0`
    
- **Data Format**: `application/json` (except for specific file upload/download endpoints)
    
- **Error Handling Convention**: All errors follow the **RFC 9457 Problem Details** standard. Backend implementations MUST use the `problem` mechanism from the `github.com/NYCU-SDC/summer` package to construct and return these errors.
    

## 📚 Table of Contents

1. [Shared Models](#1-shared-models "null")
    
2. [Auth](#2-auth "null")
    
3. [Questions](#3-questions "null")
    
4. [Chat & SSE Streaming](#4-chat--sse-streaming "null")
    
5. [Healthz](#5-healthz "null")
    

## 1. Shared Models

### ProblemDetail (Error Response Format)

All `4xx` and `5xx` errors return this format.

```
{
  "type": "[https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/404](https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/404)",
  "title": "Not Found",
  "status": 404,
  "detail": "Resource not found",
  "instance": "/api/questions/123"
}
```

### UUID & Timestamps

- **UUID**: String format UUIDv4.
    
- **utcDateTime**: ISO 8601 formatted UTC time string (e.g., `2025-10-14T12:00:00Z`).
    

## 2. Auth

Responsible for development-phase login and token refreshing.

### 2.1 Developer Login (Dev Login)

- **POST** `/auth/dev/login`
    
- **Description**: Bypasses OAuth and issues a Token directly (for development environment only; the email must already exist in the database).
    
- **Request Body**:
    
    ```
    { "email": "user@example.com" }
    ```
    
- **Response (200 OK)**:
    
    ```
    {
      "accessToken": "string",
      "refreshToken": "string",
      "expirationTime": 1735689600
    }
    ```
    

### 2.2 Refresh Token

- **POST** `/auth/refreshToken/{refreshToken}`
    
- **Description**: Exchanges the refresh token for a new Access Token.
    
- **Response (200 OK)**: Same as the `AuthToken` model.
    
- **Errors**: `401 Unauthorized`
    

## 3. Questions

### 3.1 Get All Questions

- **GET** `/questions`
    
- **Response (200 OK)**:
    
    ```
    [
      {
        "id": "uuid",
        "type": "CHOICE", // or "TEXT"
        "content": "Question content...",
        "options": [
          { "id": "uuid", "label": "A", "content": "Option A content" }
        ]
      }
    ]
    ```
    

### 3.2 Get a Single Question

- **GET** `/questions/{id}`
    
- **Parameters**: `id` (integer) _Note: Defined as an integer in the spec, please verify if this conflicts with UUID in implementation._
    
- **Response (200 OK)**: Returns a single `Question`.
    

### 3.3 Create a Question

- **POST** `/questions`
    
- **Request Body**:
    
    ```
    {
      "type": "CHOICE",
      "content": "Question content",
      "options": [
        { "label": "A", "content": "Option one" }
      ]
    }
    ```
    
- **Response (201 Created)**: Returns the created `Question` object.
    

### 3.4 Update a Question

- **PUT** `/questions/{id}`
    
- **Description**: Full update (replaces existing options entirely).
    
- **Request Body**: Same as the Create Question body.
    
- **Response (200 OK)**: Returns the updated `Question`.
    

### 3.5 Delete a Question

- **DELETE** `/questions/{id}`
    
- **Response (204 No Content)**
    

### 3.6 Get All Answers for a Question (Experimenter View)

- **GET** `/questions/{id}/answers`
    
- **Parameters**: `id` (uuid)
    
- **Response (200 OK)**: Returns an array of `Answer` objects.
    

### 3.7 Submit an Answer

- **POST** `/questions/{id}/answers`
    
- **Parameters**: `id` (uuid)
    
- **Request Body** (Provide fields based on question type):
    
    ```
    {
      "selectedOptionId": 123,
      "textAnswer": "If it is a short answer question, fill it in here"
    }
    ```
    
- **Response (201 Created)**: Returns the created `Answer`.
    

## 4. Chat & SSE Streaming

### 4.1 Create a Chat Room

- **POST** `/chat`
    
- **Response (201 Created)**:
    
    ```
    { "chatID": "uuid" }
    ```
    

### 4.2 Get Chat Message List

- **GET** `/chat/{chatId}/messages`
    
- **Response (200 OK)**:
    
    ```
    {
      "messages": [
        {
          "id": "uuid",
          "content": "Hi!",
          "role": "user", // "user" or "assistant"
          "previousID": "uuid", // optional
          "status": "done", // "streaming", "done", "error"
          "createdAt": "2025-10-14T10:00:00Z"
        }
      ]
    }
    ```
    

### 4.3 Send a Chat Message

- **POST** `/chat/{chatId}/messages`
    
- **Description**: After sending a user message, a reserved `assistant` message in the `streaming` state will be pre-created.
    
- **Request Body**:
    
    ```
    {
      "content": "Please help me answer this question",
      "previousID": "uuid" // optional
    }
    ```
    
- **Response (201 Created)**:
    
    ```
    {
      "message": { /* The message object just sent by the user */ },
      "replyMessageID": "uuid" // Use this ID to call the SSE stream API
    }
    ```
    

### 4.4 Receive Assistant Response Stream (Server-Sent Events)

- **GET** `/chat/stream/{messageId}`
    
- **Description**: Establish a connection using the `replyMessageID` obtained from the previous API.
    
- **Headers**:
    
    - `Content-Type: text/event-stream`
        
    - `Cache-Control: no-cache`
        
    - `Connection: keep-alive`
        
- **Event Stream Data**:
    
    ```
    { "content": "This is a stream" }
    { "content": " text fragment" }
    ```
    

## 5. Healthz

### 5.1 System Health Check

- **GET** `/healthz`
    
- **Response (200 OK)**: Server is running normally.